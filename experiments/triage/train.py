"""Train a local, direction-aware head on frozen NLI encoder embeddings."""

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

BASE = "cross-encoder/nli-deberta-v3-xsmall"
REVISION = "a150876415327c80daeff35ca6f68f5ed8cf5c24"
LABELS = ["abstain", "fyi", "needs_reply", "noise", "waiting_on_them"]
LABEL_POLICIES = {"synthetic-obligations-v1", "reply-triage-v2"}
HEAD_VERSION = 3
FEATURE_LAYOUT = "joint-thread-v1"
CONTEXT_TOKENS_MAX = 512
CHECKPOINT_SELECTION = "lowest temperature-calibrated development cross entropy, including epoch zero; ties keep earliest epoch"
TEMPERATURE_BOUNDS = [0.25, 8.0]
TEMPERATURE_SELECTION = "minimum development negative log likelihood; bounded log-temperature optimization, max 100 iterations, xatol 1e-6; retain one unless improved"
THRESHOLD_GRID = [
    0.0,
    *[i / 100 for i in range(40, 100, 5)],
    0.975,
    0.99,
    0.995,
    0.999,
    1.0,
]
THRESHOLD_SELECTION = "maximum development coverage per predicted class with zero accepted errors on the fixed classwise grid; unsupported classes abstain"
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
    return load_dataset(path)[0]


def label_policy(document: dict) -> str:
    policy = document.get("label_policy", "synthetic-obligations-v1")
    if not isinstance(policy, str) or policy not in LABEL_POLICIES:
        raise ValueError("unsupported label policy")
    return policy


def load_dataset(path: Path) -> tuple[list[dict], str]:
    if not path.is_file() or path.stat().st_size > 16 * 1024 * 1024:
        raise ValueError("dataset must be a regular file of at most 16 MiB")
    document = json.loads(path.read_text())
    policy = label_policy(document)
    cases = document["cases"]
    if not 1 <= len(cases) <= 10000:
        raise ValueError("expected 1–10000 cases")
    identifiers = set()
    for case in cases:
        if "label_policy" in case and label_policy(case) != policy:
            raise ValueError("case label policy differs from dataset policy")
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
    return cases, policy


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


def thread_inputs(
    encoder: "SentenceTransformer", cases: list[dict]
) -> tuple[list[str], np.ndarray]:
    if encoder.max_seq_length != CONTEXT_TOKENS_MAX:
        raise ValueError("encoder must preserve the 512-token joint context")
    if not 1 <= len(cases) <= 3000:
        raise ValueError("expected 1–3000 cases")
    texts, present = [], np.zeros((len(cases), 4), dtype=np.float32)
    for row, case in enumerate(cases):
        if not 1 <= len(case["messages"]) <= 2:
            raise ValueError("expected one or two messages")
        messages = []
        for position, message in enumerate(case["messages"]):
            speaker = "you" if message["outbound"] else "the other person"
            messages.append(f"Message from {speaker}:\n{message['body']}")
            present[
                row,
                (2 - len(case["messages"]) + position) * 2 + int(message["outbound"]),
            ] = 1
        text = "\n\n---\n\n".join(messages)
        if len(encoder.tokenizer.encode(text, truncation=False)) > CONTEXT_TOKENS_MAX:
            raise ValueError("joint thread exceeds encoder token limit")
        texts.append(text)
    return texts, present


def features(encoder: "SentenceTransformer", cases: list[dict]) -> np.ndarray:
    texts, present = thread_inputs(encoder, cases)
    vectors = encoder.encode(
        texts, batch_size=32, normalize_embeddings=True, show_progress_bar=False
    )
    if vectors.shape != (len(cases), 384):
        raise ValueError("unexpected encoder dimensions")
    return np.concatenate((vectors, present), axis=1)


def probabilities(head: dict, matrix: np.ndarray) -> np.ndarray:
    logits = matrix @ np.asarray(head["coefficients"]).T + np.asarray(
        head["intercepts"]
    )
    logits -= logits.max(axis=1, keepdims=True)
    values = np.exp(logits)
    return values / values.sum(axis=1, keepdims=True)


def validate_thresholds(thresholds: dict) -> None:
    if not isinstance(thresholds, dict) or set(thresholds) != set(LABELS):
        raise ValueError("thresholds must contain exactly the five labels")
    for threshold in thresholds.values():
        if (
            type(threshold) not in (int, float)
            or not np.isfinite(threshold)
            or not 0 <= threshold <= 1
        ):
            raise ValueError("thresholds must be finite numbers between zero and one")
    if thresholds["abstain"] != 1:
        raise ValueError("abstain threshold must be one")


def predictions(
    head: dict, values: np.ndarray, thresholds: dict | None = None
) -> list[str]:
    if thresholds is not None:
        validate_thresholds(thresholds)
    if (
        values.ndim != 2
        or values.shape[1] != len(head["labels"])
        or not np.isfinite(values).all()
    ):
        raise ValueError("invalid probability matrix")
    result = []
    for row in values:
        label = head["labels"][int(row.argmax())]
        if thresholds is not None:
            threshold = thresholds[label]
            if threshold == 1 or float(row.max()) < threshold:
                label = "abstain"
        result.append(label)
    return result


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


def load_base_encoder() -> "SentenceTransformer":
    from huggingface_hub import snapshot_download
    from sentence_transformers import SentenceTransformer
    from sentence_transformers.base.modules.transformer import Transformer
    from sentence_transformers.sentence_transformer.modules.pooling import Pooling

    snapshot = snapshot_download(
        BASE,
        revision=REVISION,
        local_files_only=True,
        allow_patterns=[
            "config.json",
            "model.safetensors",
            "tokenizer_config.json",
            "tokenizer.json",
            "special_tokens_map.json",
            "added_tokens.json",
        ],
    )
    transformer = Transformer(
        snapshot,
        model_kwargs={
            "use_safetensors": True,
            "trust_remote_code": False,
            "local_files_only": True,
        },
        processor_kwargs={"trust_remote_code": False, "local_files_only": True},
        max_seq_length=CONTEXT_TOKENS_MAX,
    )
    config = transformer.auto_model.config
    if (
        config.hidden_size != 384
        or config.max_position_embeddings != CONTEXT_TOKENS_MAX
    ):
        raise ValueError("unexpected base encoder dimensions or positional capacity")
    encoder = SentenceTransformer(
        modules=[transformer, Pooling(384, pooling_mode="mean")], device="cpu"
    )
    if (
        encoder.max_seq_length != CONTEXT_TOKENS_MAX
        or encoder.get_embedding_dimension() != 384
    ):
        raise ValueError("unexpected pooled encoder dimensions or token limit")
    return encoder


def train_model(data: Path, output: Path) -> None:

    cases, policy = load_dataset(data)
    train, dev = training_split(cases)
    output.mkdir(parents=True, exist_ok=False)
    encoder = load_base_encoder()
    position_capacity = encoder[0].auto_model.config.max_position_embeddings
    train_vectors = features(encoder, train)
    classifier = fit_classifier(train_vectors, [case["expected"] for case in train])
    head = {
        "version": HEAD_VERSION,
        "feature_layout": FEATURE_LAYOUT,
        "label_policy": policy,
        "base": BASE,
        "revision": REVISION,
        "encoder_frozen": True,
        "labels": classifier.classes_.tolist(),
        "coefficients": classifier.coef_.tolist(),
        "intercepts": classifier.intercept_.tolist(),
    }
    values = probabilities(head, features(encoder, dev))
    head["thresholds"] = select_thresholds(head, dev, values)
    report = {
        "label_policy": policy,
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
        "feature_layout": FEATURE_LAYOUT,
        "max_seq_length": CONTEXT_TOKENS_MAX,
        "base_position_capacity": position_capacity,
        "training_scenarios": len({case["group"] for case in train}),
        "dev_scenarios": len({case["group"] for case in dev}),
        "data_sha256": hashlib.sha256(data.read_bytes()).hexdigest(),
        "train_count": len(train),
        "dev_count": len(dev),
        "thresholds": head["thresholds"],
        "threshold_selection": THRESHOLD_SELECTION,
        "dev_raw": score(dev, predictions(head, values)),
        "dev_selective": score(dev, predictions(head, values, head["thresholds"])),
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


def select_thresholds(head: dict, cases: list[dict], values: np.ndarray) -> dict:
    raw = predictions(head, values)
    if len(cases) != len(raw):
        raise ValueError("prediction count mismatch")
    thresholds = dict.fromkeys(LABELS, 1.0)
    for label in LABELS:
        if label == "abstain":
            continue
        candidates = []
        for threshold in THRESHOLD_GRID[:-1]:
            accepted = [
                case
                for case, predicted, row in zip(cases, raw, values, strict=True)
                if predicted == label and float(row.max()) >= threshold
            ]
            if accepted and all(case["expected"] == label for case in accepted):
                candidates.append((len(accepted), -threshold))
        if candidates:
            thresholds[label] = -max(candidates)[1]
    return thresholds


def predict_model(model: Path, data: Path, output: Path) -> None:
    from sentence_transformers import SentenceTransformer

    cases, policy = load_dataset(data)
    from shadow import load_artifact

    head, _ = load_artifact(model)
    if label_policy(head) != policy:
        raise ValueError("model and dataset label policies differ")
    encoder = SentenceTransformer(
        str(model / "encoder"),
        device="cpu",
        local_files_only=True,
        trust_remote_code=False,
    )
    values = probabilities(head, features(encoder, cases))
    labels = predictions(head, values, head["thresholds"])
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
                    "raw": score(cases, predictions(head, values)),
                    "selective": score(cases, labels),
                },
                indent=2,
            )
        )


if __name__ == "__main__":
    main()
