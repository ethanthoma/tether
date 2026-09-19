"""Evaluate one frozen v2 candidate against its independently reviewed test set."""

import argparse
import hashlib
import json
import math
from pathlib import Path

from calibrate import check_separation
from shadow import load_artifact
from train import (
    BASE,
    CONTEXT_TOKENS_MAX,
    FEATURE_LAYOUT,
    LABELS,
    REVISION,
    THRESHOLD_SELECTION,
    features,
    load_dataset,
    predictions,
    probabilities,
    score,
    select_thresholds,
    training_split,
)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", type=Path, required=True)
    parser.add_argument("--data", type=Path, required=True)
    parser.add_argument("--training-data", type=Path, required=True)
    parser.add_argument("--plan", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    cases, policy = load_dataset(args.data)
    if policy != "reply-triage-v2":
        raise ValueError("v2 evaluation policy required")
    validate_test(cases)
    validate_reviewed_source(json.loads(args.data.read_text()))
    training_cases, training_policy = load_dataset(args.training_data)
    validate_reviewed_source(json.loads(args.training_data.read_text()))
    if training_policy != policy:
        raise ValueError("training and test policies differ")
    train, dev = training_split(training_cases)
    separation = check_separation([train, dev, cases])
    model = args.model.resolve(strict=True)
    head, identity = load_artifact(model)
    if head.get("label_policy") != policy:
        raise ValueError("model and test policies differ")
    training_bytes = (model / "training.json").read_bytes()
    training_hash = hashlib.sha256(args.training_data.read_bytes()).hexdigest()
    training_report = json.loads(training_bytes)
    validate_training(training_report, head, training_hash, len(train), len(dev))
    import torch
    from sentence_transformers import SentenceTransformer

    torch.set_num_threads(4)
    encoder = SentenceTransformer(
        str(model / "encoder"),
        device="cpu",
        local_files_only=True,
        trust_remote_code=False,
    )
    if (
        encoder.max_seq_length != CONTEXT_TOKENS_MAX
        or encoder[0].auto_model.config.max_position_embeddings != CONTEXT_TOKENS_MAX
    ):
        raise ValueError("encoder context limit differs from release recipe")
    development = probabilities(head, features(encoder, dev))
    if select_thresholds(head, dev, development) != head["thresholds"]:
        raise ValueError("artifact cutoffs do not match development-only selection")
    readiness = validate_development(
        dev, predictions(head, development, head["thresholds"])
    )
    values = probabilities(head, features(encoder, cases))
    raw = predictions(head, values)
    selected = predictions(head, values, head["thresholds"])
    report = {
        "version": 1,
        "label_policy": policy,
        "model_sha256": identity,
        "test_sha256": hashlib.sha256(args.data.read_bytes()).hexdigest(),
        "plan_sha256": hashlib.sha256(args.plan.read_bytes()).hexdigest(),
        "training_report_sha256": hashlib.sha256(training_bytes).hexdigest(),
        "training_data_sha256": training_hash,
        "train_count": len(train),
        "dev_count": len(dev),
        "feature_layout": FEATURE_LAYOUT,
        "max_seq_length": CONTEXT_TOKENS_MAX,
        "separation": separation,
        "thresholds": head["thresholds"],
        "development_readiness": readiness,
        **release_metrics(cases, selected, raw),
        "limitations": "Synthetic, correlated families; not an estimate of real-mail accuracy.",
        "predictions": [
            {
                "id": case["id"],
                "expected": case["expected"],
                "raw": raw_label,
                "selected": label,
            }
            for case, raw_label, label in zip(cases, raw, selected, strict=True)
        ],
    }
    with args.output.open("x") as output:
        output.write(json.dumps(report, indent=2) + "\n")
    print(
        json.dumps(
            {key: value for key, value in report.items() if key != "predictions"},
            indent=2,
        )
    )
    if not report["approved"]:
        raise SystemExit(1)


def validate_reviewed_source(source: dict) -> None:
    if (
        source.get("provenance") != "fully_synthetic_assistant_authored"
        or source.get("review_status")
        != "blind_reviewer_agreed_independence_self_attested"
    ):
        raise ValueError("independently reviewed fully synthetic export required")


def validate_development(cases: list[dict], selected: list[str]) -> dict:
    if not 1 <= len(cases) <= 1000 or any(case.get("split") != "dev" for case in cases):
        raise ValueError("development-only cases required")
    if any(label not in LABELS for label in selected):
        raise ValueError("unknown development prediction")
    metrics = score(cases, selected)
    errors = metrics["accepted"] - metrics["correct_accepted"]
    coverage = metrics["accepted"] / len(cases)
    actionable = {
        label: sum(
            case["expected"] == prediction == label
            for case, prediction in zip(cases, selected, strict=True)
        )
        for label in ("needs_reply", "waiting_on_them")
    }
    if errors or coverage < 0.25 or not all(actionable.values()):
        raise ValueError(
            "development readiness failed; held-out inference is prohibited"
        )
    return {
        "cases": len(cases),
        "accepted": metrics["accepted"],
        "accepted_errors": errors,
        "coverage": coverage,
        "actionable_correct": actionable,
    }


def validate_training(
    report: dict, head: dict, data_hash: str, train_count: int, dev_count: int
) -> None:
    expected = {
        "label_policy": "reply-triage-v2",
        "feature_layout": FEATURE_LAYOUT,
        "max_seq_length": CONTEXT_TOKENS_MAX,
        "base_position_capacity": CONTEXT_TOKENS_MAX,
        "base": BASE,
        "revision": REVISION,
        "data_sha256": data_hash,
        "train_count": train_count,
        "dev_count": dev_count,
        "seed": 42,
        "epochs": 12,
        "batch_size": 16,
        "encoder_learning_rate": 2e-5,
        "head_learning_rate": 1e-3,
        "weight_decay": 0.01,
        "gradient_norm_max": 1.0,
        "loss": "inverse-frequency-weighted cross entropy",
        "head_initialization": "frozen-encoder logistic regression",
        "checkpoint_selection": "lowest unweighted development cross entropy, including epoch zero",
        "threshold_selection": THRESHOLD_SELECTION,
        "thresholds": head["thresholds"],
    }
    for key, value in expected.items():
        if type(report.get(key)) is not type(value) or report[key] != value:
            raise ValueError(f"training metadata mismatch: {key}")
    epoch = report.get("selected_epoch")
    history = report.get("history")
    if type(epoch) is not int or not 0 <= epoch <= 12:
        raise ValueError("invalid selected epoch")
    if not isinstance(history, list) or len(history) != 13:
        raise ValueError("complete epoch-zero through epoch-twelve history required")
    for index, row in enumerate(history):
        if (
            not isinstance(row, dict)
            or type(row.get("epoch")) is not int
            or row["epoch"] != index
            or type(row.get("dev_loss")) not in (int, float)
            or not math.isfinite(row["dev_loss"])
            or row["dev_loss"] < 0
        ):
            raise ValueError("invalid development history")
    if epoch != min(range(13), key=lambda index: history[index]["dev_loss"]):
        raise ValueError("selected epoch is not first minimum development loss")
    if report.get("encoder_frozen") is not (epoch == 0):
        raise ValueError("training encoder state differs from selected epoch")
    for key in ("base", "revision", "encoder_frozen", "feature_layout"):
        if head.get(key) != report[key]:
            raise ValueError(f"model and training metadata differ: {key}")


def validate_test(cases: list[dict]) -> None:
    if not 80 <= len(cases) <= 1000:
        raise ValueError("test requires 80–1000 cases")
    if any(
        case.get("split") != "test"
        or not isinstance(case.get("group"), str)
        or not case["group"]
        for case in cases
    ):
        raise ValueError("test-only cases with family identifiers required")
    if len({case["group"] for case in cases}) < 20:
        raise ValueError("at least 20 test families required")
    if {case.get("expected") for case in cases} != set(LABELS):
        raise ValueError("all five test classes required")


def release_metrics(cases: list[dict], selected: list[str], raw: list[str]) -> dict:
    validate_test(cases)
    if any(label not in LABELS for label in selected + raw):
        raise ValueError("unknown prediction label")
    selective = score(cases, selected)
    raw_metrics = score(cases, raw)
    errors = selective["accepted"] - selective["correct_accepted"]
    coverage = selective["accepted"] / len(cases)
    per_class = {}
    for expected in LABELS:
        pairs = [
            (case, label)
            for case, label in zip(cases, selected, strict=True)
            if case["expected"] == expected
        ]
        per_class[expected] = score(
            [case for case, _ in pairs], [label for _, label in pairs]
        )
    actionable = all(
        per_class[label]["correct_accepted"] >= 1
        for label in ("needs_reply", "waiting_on_them")
    )
    return {
        "approved": errors == 0 and coverage >= 0.25 and actionable,
        "families": len({case["group"] for case in cases}),
        "coverage": coverage,
        "accepted_errors": errors,
        "raw_accuracy": (
            raw_metrics["correct_accepted"] + raw_metrics["correct_abstentions"]
        )
        / len(cases),
        "raw": raw_metrics,
        "selective": selective,
        "per_class": per_class,
    }


if __name__ == "__main__":
    main()
