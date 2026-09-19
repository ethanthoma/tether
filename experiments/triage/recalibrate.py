"""Tighten one frozen classifier's cutoffs and evaluate one fresh holdout."""

import argparse
import hashlib
import json
import os
import shutil
from pathlib import Path

import numpy as np
from calibrate import check_separation
from release import (
    read_unscaled_head,
    release_metrics,
    validate_calibration,
    validate_development,
    validate_reviewed_source,
    validate_test,
    validate_training,
)
from shadow import load_artifact
from train import (
    CONTEXT_TOKENS_MAX,
    LABELS,
    THRESHOLD_GRID,
    features,
    load_dataset,
    predictions,
    probabilities,
    score,
    select_thresholds,
    training_split,
    validate_thresholds,
)

SOURCE_SHA256 = "951f1830d3c52e5f899d080776fecd00b5b66ff39c3ca6cb85c8e3731fe00bac"
TRAINING_DATA_SHA256 = (
    "092276e37bfed1fcdcac6fc0013fb1176759fb6522135b1df7a75c6eb78c0cd4"
)
TRAINING_REPORT_SHA256 = (
    "970c690719d621d21c6b54f21fa4256c53f9e88b3e627ec5e4a62124b2115e95"
)
CUTOFF_RULE = "maximum source and fresh-calibration zero-error grid cutoff; actionable classes advance one grid step then floor at 0.90; abstain stays one"


def main() -> None:
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    for name in ("fit", "evaluate"):
        command = commands.add_parser(name)
        for argument in (
            "source-model",
            "training-data",
            "calibration-data",
            "plan",
            "output",
        ):
            command.add_argument(f"--{argument}", required=True, type=Path)
        if name == "evaluate":
            command.add_argument("--model", required=True, type=Path)
            command.add_argument("--data", required=True, type=Path)
    args = parser.parse_args()
    if args.output.exists():
        raise FileExistsError(args.output)
    context = prepare(
        args.source_model, args.training_data, args.calibration_data, args.plan
    )
    if args.command == "fit":
        if not fit(context, args.output)["approved"]:
            raise SystemExit(1)
    else:
        report = evaluate(context, args.model, args.data, args.output)
        if not report["approved"]:
            raise SystemExit(1)


def cutoff_thresholds(source: dict, cases: list[dict], values: np.ndarray) -> dict:
    validate_thresholds(source["thresholds"])
    selected = select_thresholds(source, cases, values)
    result = {}
    for label in LABELS:
        threshold = max(source["thresholds"][label], selected[label])
        if threshold not in THRESHOLD_GRID:
            raise ValueError("source cutoff is not on the frozen grid")
        if label in ("needs_reply", "waiting_on_them"):
            index = THRESHOLD_GRID.index(threshold)
            threshold = max(
                0.90, THRESHOLD_GRID[min(index + 1, len(THRESHOLD_GRID) - 1)]
            )
        result[label] = threshold
    result["abstain"] = 1.0
    validate_thresholds(result)
    return result


def reviewed_cases(path: Path) -> list[dict]:
    cases, policy = load_dataset(path)
    if policy != "reply-triage-v2":
        raise ValueError("v2 policy required")
    validate_reviewed_source(json.loads(path.read_text()))
    return cases


def prepare(
    source_model: Path, training_data: Path, calibration_data: Path, plan: Path
) -> dict:
    source_model = source_model.resolve(strict=True)
    source, identity = load_artifact(source_model)
    if identity != SOURCE_SHA256:
        raise ValueError("unapproved source model identity")
    if not training_data.is_file() or training_data.stat().st_size > 16 * 1024 * 1024:
        raise ValueError(
            "source training data must be a regular file of at most 16 MiB"
        )
    training_hash = hashlib.sha256(training_data.read_bytes()).hexdigest()
    if training_hash != TRAINING_DATA_SHA256:
        raise ValueError("source training data differs from the frozen identity")
    training_bytes = read_unscaled_head(source_model / "training.json")
    if hashlib.sha256(training_bytes).hexdigest() != TRAINING_REPORT_SHA256:
        raise ValueError("source training report differs from the frozen identity")
    train, dev = training_split(reviewed_cases(training_data))
    calibration = reviewed_cases(calibration_data)
    if not 300 <= len(calibration) <= 1000 or any(
        case.get("split") != "dev"
        or not isinstance(case.get("group"), str)
        or not case["group"]
        for case in calibration
    ):
        raise ValueError("fresh development-only calibration families required")
    if len({case["group"] for case in calibration}) < 150:
        raise ValueError("at least 150 calibration families required")
    if {case["expected"] for case in calibration} != set(LABELS):
        raise ValueError("all five calibration classes required")
    separation = check_separation([train, dev, calibration])
    unscaled_bytes = read_unscaled_head(source_model / "unscaled-head.json")
    report = json.loads(training_bytes)
    validate_training(report, source, training_hash, len(train), len(dev))

    import torch
    from sentence_transformers import SentenceTransformer

    torch.set_num_threads(4)
    encoder = SentenceTransformer(
        str(source_model / "encoder"),
        device="cpu",
        local_files_only=True,
        trust_remote_code=False,
    )
    if (
        encoder.max_seq_length != CONTEXT_TOKENS_MAX
        or encoder[0].auto_model.config.max_position_embeddings != CONTEXT_TOKENS_MAX
    ):
        raise ValueError("unexpected encoder context limit")
    original_values = validate_calibration(
        report, source, unscaled_bytes, dev, features(encoder, dev)
    )
    if select_thresholds(source, dev, original_values) != source["thresholds"]:
        raise ValueError("source cutoffs do not match original development selection")
    validate_development(
        dev, predictions(source, original_values, source["thresholds"])
    )
    calibration_values = probabilities(source, features(encoder, calibration))
    thresholds = cutoff_thresholds(source, calibration, calibration_values)
    provenance = {
        "source_model_sha256": identity,
        "training_data_sha256": training_hash,
        "training_report_sha256": TRAINING_REPORT_SHA256,
        "calibration_data_sha256": hashlib.sha256(
            calibration_data.read_bytes()
        ).hexdigest(),
        "plan_sha256": hashlib.sha256(plan.read_bytes()).hexdigest(),
        "rule": CUTOFF_RULE,
    }
    candidate = {**source, "thresholds": thresholds, "cutoff_calibration": provenance}
    original_readiness = readiness(
        dev, predictions(candidate, original_values, thresholds)
    )
    calibration_readiness = readiness(
        calibration, predictions(candidate, calibration_values, thresholds)
    )
    return {
        "source_model": source_model,
        "source": source,
        "candidate": candidate,
        "encoder": encoder,
        "train": train,
        "dev": dev,
        "calibration": calibration,
        "training_bytes": training_bytes,
        "unscaled_bytes": unscaled_bytes,
        "provenance": provenance,
        "separation": separation,
        "original_development_readiness": original_readiness,
        "calibration_readiness": calibration_readiness,
    }


def readiness(cases: list[dict], selected: list[str]) -> dict:
    try:
        return {"approved": True, **validate_development(cases, selected)}
    except ValueError as error:
        metrics = score(cases, selected)
        return {
            "approved": False,
            "reason": str(error),
            "cases": len(cases),
            "accepted": metrics["accepted"],
            "accepted_errors": metrics["accepted"] - metrics["correct_accepted"],
            "coverage": metrics["accepted"] / len(cases),
            "actionable_correct": {
                label: sum(
                    case["expected"] == predicted == label
                    for case, predicted in zip(cases, selected, strict=True)
                )
                for label in ("needs_reply", "waiting_on_them")
            },
        }


def encoder_hashes(model: Path) -> dict[str, str]:
    files = sorted(path for path in (model / "encoder").rglob("*") if path.is_file())
    if (
        not 1 <= len(files) <= 128
        or sum(path.stat().st_size for path in files) > 512 * 1024 * 1024
    ):
        raise ValueError("encoder bounds exceeded")
    result = {}
    for path in files:
        if path.is_symlink():
            raise ValueError("linked encoder file")
        digest = hashlib.sha256()
        with path.open("rb") as stream:
            for _ in range((path.stat().st_size + 65535) // 65536):
                digest.update(stream.read(65536))
        result[path.relative_to(model / "encoder").as_posix()] = digest.hexdigest()
    return result


def validate_candidate(context: dict, model: Path) -> tuple[dict, str]:
    head, identity = load_artifact(model)
    if json.dumps(head, sort_keys=True) != json.dumps(
        context["candidate"], sort_keys=True
    ):
        raise ValueError(
            "candidate changes more than the prescribed cutoffs and provenance"
        )
    for name, content in (
        ("training.json", context["training_bytes"]),
        ("unscaled-head.json", context["unscaled_bytes"]),
    ):
        if read_unscaled_head(model / name) != content:
            raise ValueError("candidate changed original training metadata")
    if encoder_hashes(model) != encoder_hashes(context["source_model"]):
        raise ValueError("candidate changed encoder files")
    if load_artifact(context["source_model"])[1] != SOURCE_SHA256:
        raise ValueError("source changed during recalibration")
    return head, identity


def calibration_report(context: dict, identity: str) -> dict:
    return {
        "version": 1,
        "label_policy": "reply-triage-v2",
        "model_sha256": identity,
        "approved": context["original_development_readiness"]["approved"]
        and context["calibration_readiness"]["approved"],
        **context["provenance"],
        "thresholds": context["candidate"]["thresholds"],
        "original_development_readiness": context["original_development_readiness"],
        "calibration_readiness": context["calibration_readiness"],
        "separation": context["separation"],
    }


def fit(context: dict, output: Path) -> dict:
    output.mkdir(parents=True, exist_ok=False)
    shutil.copytree(context["source_model"] / "encoder", output / "encoder")
    (output / "head.json").write_text(json.dumps(context["candidate"], indent=2) + "\n")
    (output / "training.json").write_bytes(context["training_bytes"])
    (output / "unscaled-head.json").write_bytes(context["unscaled_bytes"])
    _, identity = validate_candidate(context, output)
    report = calibration_report(context, identity)
    (output / "calibration.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))
    return report


def evaluate(context: dict, model: Path, data: Path, output: Path) -> dict:
    model = model.resolve(strict=True)
    head, identity = validate_candidate(context, model)
    expected_report = calibration_report(context, identity)
    if json.loads(read_unscaled_head(model / "calibration.json")) != expected_report:
        raise ValueError(
            "candidate calibration report differs from independent recomputation"
        )
    if not expected_report["approved"]:
        raise ValueError(
            "development readiness failed; held-out inference is prohibited"
        )
    cases = reviewed_cases(data)
    validate_test(cases)
    separation = check_separation(
        [context["train"], context["dev"], context["calibration"], cases]
    )
    with output.open("x") as stream:
        values = probabilities(head, features(context["encoder"], cases))
        raw = predictions(head, values)
        selected = predictions(head, values, head["thresholds"])
        report = {
            **expected_report,
            "test_sha256": hashlib.sha256(data.read_bytes()).hexdigest(),
            "separation": separation,
            **release_metrics(cases, selected, raw),
            "limitations": "Synthetic correlated families; not an estimate of real-mail accuracy.",
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
        stream.write(json.dumps(report, indent=2) + "\n")
    print(
        json.dumps(
            {key: value for key, value in report.items() if key != "predictions"},
            indent=2,
        )
    )
    return report


if __name__ == "__main__":
    main()
