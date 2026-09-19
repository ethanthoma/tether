"""Evaluate one frozen v2 candidate against its independently reviewed test set."""

import argparse
import hashlib
import json
from pathlib import Path

from shadow import load_artifact
from train import LABELS, features, load_dataset, predictions, probabilities, score


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", type=Path, required=True)
    parser.add_argument("--data", type=Path, required=True)
    parser.add_argument("--plan", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    cases, policy = load_dataset(args.data)
    if policy != "reply-triage-v2":
        raise ValueError("v2 evaluation policy required")
    validate_test(cases)
    source = json.loads(args.data.read_text())
    if (
        source.get("review_status")
        != "blind_reviewer_agreed_independence_self_attested"
    ):
        raise ValueError("independently reviewed test export required")
    model = args.model.resolve(strict=True)
    head, identity = load_artifact(model)
    if head.get("label_policy") != policy:
        raise ValueError("model and test policies differ")
    import torch
    from sentence_transformers import SentenceTransformer

    torch.set_num_threads(4)
    encoder = SentenceTransformer(
        str(model / "encoder"),
        device="cpu",
        local_files_only=True,
        trust_remote_code=False,
    )
    values = probabilities(head, features(encoder, cases))
    raw = predictions(head, values, 0)
    selected = predictions(head, values, head["threshold"])
    report = {
        "version": 1,
        "label_policy": policy,
        "model_sha256": identity,
        "test_sha256": hashlib.sha256(args.data.read_bytes()).hexdigest(),
        "plan_sha256": hashlib.sha256(args.plan.read_bytes()).hexdigest(),
        "threshold": head["threshold"],
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
