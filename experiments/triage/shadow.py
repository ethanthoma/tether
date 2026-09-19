"""Run one bounded, local-only CPU classification batch without changing state.

Artifact identity is SHA-256 over head.json followed by every regular encoder
file in sorted relative-path order. Each file contributes its UTF-8 relative
path length (8-byte big endian), path, byte length (8-byte big endian), and bytes.
"""

import argparse
import contextlib
import hashlib
import json
import os
import sys
from pathlib import Path
from typing import BinaryIO

os.environ["HF_HUB_OFFLINE"] = "1"
os.environ["TRANSFORMERS_OFFLINE"] = "1"
os.environ["HF_HUB_DISABLE_TELEMETRY"] = "1"
os.environ["TOKENIZERS_PARALLELISM"] = "false"

import numpy as np
from train import LABELS, features, label_policy, predictions, probabilities

REQUEST_BYTES_MAX = 256 * 1024


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", required=True, type=Path)
    args = parser.parse_args()
    try:
        cases = read_request(sys.stdin.buffer)
        with (
            open(os.devnull, "w") as quiet,
            contextlib.redirect_stdout(quiet),
            contextlib.redirect_stderr(quiet),
        ):
            model = args.model.resolve(strict=True)
            head, identity = load_artifact(model)
            import torch
            from sentence_transformers import SentenceTransformer

            torch.set_num_threads(4)
            encoder = SentenceTransformer(
                str(model / "encoder"),
                device="cpu",
                local_files_only=True,
                trust_remote_code=False,
            )
            encoder.eval()
            result = classify(cases, head, encoder, identity)
        print(json.dumps(result, allow_nan=False, separators=(",", ":")))
    except Exception:  # noqa: BLE001 -- Third-party errors must never expose message text.
        print("shadow inference failed", file=sys.stderr)
        raise SystemExit(1) from None


def read_request(source: BinaryIO) -> list[dict]:
    payload = source.read(REQUEST_BYTES_MAX + 1)
    if len(payload) > REQUEST_BYTES_MAX:
        raise ValueError("request limit exceeded")
    request = json.loads(payload)
    if not isinstance(request, dict) or type(request.get("version")) is not int:
        raise ValueError("invalid request")
    cases = request.get("cases")
    if (
        request["version"] != 1
        or not isinstance(cases, list)
        or not 1 <= len(cases) <= 20
    ):
        raise ValueError("invalid request")
    identifiers = set()
    for case in cases:
        if not isinstance(case, dict):
            raise TypeError("invalid case")
        identifier = case.get("id")
        if not isinstance(identifier, str) or not 1 <= len(identifier.encode()) <= 128:
            raise ValueError("invalid identifier")
        if identifier in identifiers:
            raise ValueError("duplicate identifier")
        identifiers.add(identifier)
    return cases


def load_artifact(model: Path) -> tuple[dict, str]:
    head_path = model / "head.json"
    if (
        head_path.is_symlink()
        or not head_path.is_file()
        or head_path.stat().st_size > 1024 * 1024
    ):
        raise ValueError("invalid head")
    head = json.loads(head_path.read_bytes())
    validate_head(head)
    encoder = model / "encoder"
    if (
        encoder.is_symlink()
        or not encoder.is_dir()
        or not (encoder / "model.safetensors").is_file()
    ):
        raise ValueError("invalid encoder")
    paths = [head_path]
    for directories_count, (directory, directories, files) in enumerate(
        os.walk(encoder, followlinks=False), 1
    ):
        if any((Path(directory) / name).is_symlink() for name in directories):
            raise ValueError("linked encoder directory")
        paths.extend(Path(directory) / name for name in files)
        if len(paths) + len(directories) + directories_count > 128:
            raise ValueError("encoder file limit exceeded")
    paths = [head_path] + sorted(
        paths[1:], key=lambda path: path.relative_to(model).as_posix()
    )
    digest = hashlib.sha256()
    total = 0
    for path in paths:
        if path.is_symlink() or not path.is_file():
            raise ValueError("invalid encoder file")
        size = path.stat().st_size
        total += size
        if total > 512 * 1024 * 1024:
            raise ValueError("encoder size limit exceeded")
        name = path.relative_to(model).as_posix().encode()
        digest.update(len(name).to_bytes(8, "big"))
        digest.update(name)
        digest.update(size.to_bytes(8, "big"))
        with path.open("rb") as source:
            for _ in range((size + 65535) // 65536):
                digest.update(source.read(65536))
            if source.tell() != size or source.read(1):
                raise ValueError("artifact changed while reading")
    return head, digest.hexdigest()


def validate_head(head: dict) -> None:
    if (
        not isinstance(head, dict)
        or type(head.get("version")) is not int
        or head["version"] != 1
    ):
        raise ValueError("invalid head version")
    label_policy(head)
    labels = head.get("labels")
    if not isinstance(labels, list) or len(labels) != 5 or set(labels) != set(LABELS):
        raise ValueError("invalid head labels")
    threshold = head.get("threshold")
    if (
        type(threshold) not in (int, float)
        or not np.isfinite(threshold)
        or not 0 <= threshold <= 1
    ):
        raise ValueError("invalid head threshold")
    for field, shape in (("coefficients", (5, 1540)), ("intercepts", (5,))):
        values = np.asarray(head.get(field), dtype=np.float64)
        if values.shape != shape or not np.isfinite(values).all():
            raise ValueError("invalid head weights")


def valid_messages(case: dict, encoder: object) -> bool:
    messages = case.get("messages")
    if not isinstance(messages, list) or not 1 <= len(messages) <= 2:
        return False
    for message in messages:
        if not isinstance(message, dict) or type(message.get("outbound")) is not bool:
            return False
        body = message.get("body")
        if not isinstance(body, str) or not body.strip():
            return False
        try:
            body_size = len(body.encode())
        except UnicodeEncodeError:
            return False
        if body_size > 4000:
            return False
        if len(encoder.tokenizer.encode(body)) > encoder.max_seq_length:
            return False
    return True


def classify(cases: list[dict], head: dict, encoder: object, identity: str) -> dict:
    validate_head(head)
    results = [
        {
            "id": case["id"],
            "label": "abstain",
            "confidence": 0,
            "status": "input_rejected",
        }
        for case in cases
    ]
    accepted = [
        index for index, case in enumerate(cases) if valid_messages(case, encoder)
    ]
    if accepted:
        matrix = features(encoder, [cases[index] for index in accepted])
        if matrix.shape != (len(accepted), 1540) or not np.isfinite(matrix).all():
            raise ValueError("invalid encoder output")
        values = probabilities(head, matrix)
        if not np.isfinite(values).all() or np.any(values < 0) or np.any(values > 1):
            raise ValueError("invalid probabilities")
        labels = predictions(head, values, head["threshold"])
        for index, label, row in zip(accepted, labels, values, strict=True):
            results[index].update(label=label, confidence=float(row.max()), status="ok")
    return {
        "version": 1,
        "policy": label_policy(head),
        "model_sha256": identity,
        "results": results,
    }


if __name__ == "__main__":
    main()
