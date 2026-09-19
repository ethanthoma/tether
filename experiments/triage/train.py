"""Train a local, direction-aware head on frozen MiniLM embeddings."""

import argparse
import hashlib
import json
import os
from pathlib import Path
from typing import TYPE_CHECKING

import numpy as np

if TYPE_CHECKING:
    from sentence_transformers import SentenceTransformer
    from sklearn.linear_model import LogisticRegression

os.environ.setdefault("HF_HUB_DISABLE_TELEMETRY", "1")
os.environ.setdefault("TOKENIZERS_PARALLELISM", "false")

BASE = "sentence-transformers/all-MiniLM-L6-v2"
REVISION = "1110a243fdf4706b3f48f1d95db1a4f5529b4d41"
LABELS = ["abstain", "fyi", "needs_reply", "noise", "waiting_on_them"]
ROOT = Path(__file__).resolve().parent


def main() -> None:
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    train = commands.add_parser("train")
    train.add_argument("--data", type=Path, default=ROOT / "seed.json")
    train.add_argument("--output", type=Path, required=True)
    predict = commands.add_parser("predict")
    predict.add_argument("--model", type=Path, required=True)
    predict.add_argument("--data", type=Path, required=True)
    predict.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()

    import torch

    torch.set_num_threads(4)
    if args.command == "train":
        train_model(args.data, args.output)
    else:
        predict_model(args.model, args.data, args.output)


def load_cases(path: Path) -> list[dict]:
    if not path.is_file() or path.stat().st_size > 16 * 1024 * 1024:
        raise ValueError("dataset must be a regular file of at most 16 MiB")
    cases = json.loads(path.read_text())["cases"]
    if not 1 <= len(cases) <= 10000:
        raise ValueError("expected 1–10000 cases")
    identifiers = set()
    for case in cases:
        identifier = case["id"]
        if (
            not isinstance(identifier, str)
            or not identifier
            or identifier in identifiers
        ):
            raise ValueError("case IDs must be unique nonempty strings")
        identifiers.add(identifier)
        if "expected" in case and case["expected"] not in LABELS:
            raise ValueError("unknown label")
        if not 1 <= len(case["messages"]) <= 2:
            raise ValueError("expected one or two chronological messages")
        for message in case["messages"]:
            if type(message["outbound"]) is not bool:
                raise ValueError("outbound must be boolean")
            body = message["body"]
            if (
                not isinstance(body, str)
                or not body.strip()
                or len(body.encode()) > 4000
            ):
                raise ValueError("body must contain 1–4000 bytes of text")
    return cases


def training_split(cases: list[dict]) -> tuple[list[dict], list[dict]]:
    groups: dict[str, str] = {}
    bodies: dict[str, str] = {}
    for case in cases:
        split, group = case["split"], case["group"]
        if split not in ("train", "dev") or not group:
            raise ValueError("each seed needs a train/dev split and scenario group")
        if groups.setdefault(group, split) != split:
            raise ValueError("scenario group crosses splits")
        for message in case["messages"]:
            body = " ".join(message["body"].lower().split())
            if bodies.setdefault(body, split) != split:
                raise ValueError("message text crosses splits")
    train = [case for case in cases if case["split"] == "train"]
    dev = [case for case in cases if case["split"] == "dev"]
    for split in (train, dev):
        if {case["expected"] for case in split} != set(LABELS):
            raise ValueError("each split must contain all labels")
    return train, dev


def features(encoder: "SentenceTransformer", cases: list[dict]) -> np.ndarray:
    texts = [message["body"] for case in cases for message in case["messages"]]
    lengths = [len(encoder.tokenizer.encode(text)) for text in texts]
    if max(lengths) > encoder.max_seq_length:
        raise ValueError(
            "message exceeds encoder token limit; refusing silent truncation"
        )
    vectors = encoder.encode(
        texts, batch_size=32, normalize_embeddings=True, show_progress_bar=False
    )
    width = vectors.shape[1] + 1
    result = np.zeros((len(cases), 4 * width), dtype=np.float32)
    index = 0
    for row, case in enumerate(cases):
        for position, message in enumerate(case["messages"]):
            slot = (2 - len(case["messages"]) + position) * 2 + int(message["outbound"])
            start = slot * width
            result[row, start : start + width - 1] = vectors[index]
            result[row, start + width - 1] = 1
            index += 1
    return result


def probabilities(head: dict, matrix: np.ndarray) -> np.ndarray:
    logits = matrix @ np.asarray(head["coefficients"]).T + np.asarray(
        head["intercepts"]
    )
    logits -= logits.max(axis=1, keepdims=True)
    values = np.exp(logits)
    return values / values.sum(axis=1, keepdims=True)


def predictions(head: dict, values: np.ndarray, threshold: float) -> list[str]:
    if not 0 <= threshold <= 1:
        raise ValueError("threshold must be between zero and one")
    if threshold == 1:
        return ["abstain"] * len(values)
    return [
        head["labels"][int(row.argmax())]
        if float(row.max()) >= threshold
        else "abstain"
        for row in values
    ]


def score(cases: list[dict], predicted: list[str]) -> dict:
    if len(cases) != len(predicted):
        raise ValueError("prediction count mismatch")
    result = dict.fromkeys(
        [
            "cases",
            "accepted",
            "correct_accepted",
            "correct_abstentions",
            "false_reminders",
            "false_silencing",
            "actionable_abstained",
            "ambiguous_accepted",
        ],
        0,
    )
    actionable = {"needs_reply", "waiting_on_them"}
    for case, label in zip(cases, predicted, strict=True):
        expected = case["expected"]
        result["cases"] += 1
        result["accepted"] += label != "abstain"
        result["correct_accepted"] += label != "abstain" and label == expected
        result["correct_abstentions"] += label == expected == "abstain"
        result["false_reminders"] += label in actionable and label != expected
        result["false_silencing"] += expected in actionable and label in {
            "fyi",
            "noise",
        }
        result["actionable_abstained"] += expected in actionable and label == "abstain"
        result["ambiguous_accepted"] += expected == "abstain" and label != "abstain"
    return result


def train_model(data: Path, output: Path) -> None:
    from sentence_transformers import SentenceTransformer

    train, dev = training_split(load_cases(data))
    output.mkdir(parents=True, exist_ok=False)
    encoder = SentenceTransformer(
        BASE, revision=REVISION, device="cpu", trust_remote_code=False
    )
    train_vectors = features(encoder, train)
    classifier = fit_classifier(train_vectors, [case["expected"] for case in train])
    head = {
        "version": 1,
        "base": BASE,
        "revision": REVISION,
        "encoder_frozen": True,
        "labels": classifier.classes_.tolist(),
        "coefficients": classifier.coef_.tolist(),
        "intercepts": classifier.intercept_.tolist(),
    }
    values = probabilities(head, features(encoder, dev))
    head["threshold"] = select_threshold(head, dev, values)
    report = {
        "base": BASE,
        "revision": REVISION,
        "encoder_frozen": True,
        "classifier": {
            "type": "logistic_regression",
            "C": 10,
            "class_weight": "balanced",
            "max_iter": 1000,
            "seed": 42,
        },
        "features": "ordered previous/latest message; separate inbound/outbound embedding and presence slots",
        "training_scenarios": len({case["group"] for case in train}),
        "dev_scenarios": len({case["group"] for case in dev}),
        "data_sha256": hashlib.sha256(data.read_bytes()).hexdigest(),
        "train_count": len(train),
        "dev_count": len(dev),
        "threshold": head["threshold"],
        "threshold_selection": "maximum dev coverage with zero accepted dev errors; ties choose lower threshold",
        "dev_raw": score(dev, predictions(head, values, 0)),
        "dev_selective": score(dev, predictions(head, values, head["threshold"])),
    }
    encoder.save_pretrained(output / "encoder", safe_serialization=True)
    (output / "head.json").write_text(json.dumps(head, indent=2) + "\n")
    (output / "training.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))


def fit_classifier(matrix: np.ndarray, labels: list[str]) -> "LogisticRegression":
    from sklearn.linear_model import LogisticRegression

    classifier = LogisticRegression(
        C=10, class_weight="balanced", max_iter=1000, random_state=42
    )
    classifier.fit(matrix, labels)
    if classifier.n_iter_.max() >= classifier.max_iter:
        raise RuntimeError("classifier did not converge")
    return classifier


def select_threshold(head: dict, cases: list[dict], values: np.ndarray) -> float:
    candidates = []
    for threshold in [0.0, *[i / 100 for i in range(40, 100, 5)], 1.0]:
        metrics = score(cases, predictions(head, values, threshold))
        if metrics["accepted"] == metrics["correct_accepted"]:
            candidates.append((metrics["accepted"], -threshold))
    return -max(candidates)[1]


def predict_model(model: Path, data: Path, output: Path) -> None:
    from sentence_transformers import SentenceTransformer

    cases = load_cases(data)
    head = json.loads((model / "head.json").read_text())
    if head["version"] != 1 or set(head["labels"]) != set(LABELS):
        raise ValueError("unsupported classifier artifact")
    encoder = SentenceTransformer(
        str(model / "encoder"),
        device="cpu",
        local_files_only=True,
        trust_remote_code=False,
    )
    values = probabilities(head, features(encoder, cases))
    labels = predictions(head, values, head["threshold"])
    with output.open("x") as target:
        json.dump(
            {case["id"]: label for case, label in zip(cases, labels, strict=True)},
            target,
            indent=2,
        )
        target.write("\n")
    if all("expected" in case for case in cases):
        print(
            json.dumps(
                {
                    "raw": score(cases, predictions(head, values, 0)),
                    "selective": score(cases, labels),
                },
                indent=2,
            )
        )


if __name__ == "__main__":
    main()
