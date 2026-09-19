"""Calibrate a fixed classifier on separate synthetic fit, selection, and audit data."""

import argparse
import hashlib
import json
import re
from pathlib import Path

import numpy as np
from scipy.optimize import minimize_scalar
from scipy.special import logsumexp
from train import (
    LABELS,
    ROOT,
    features,
    load_cases,
    predictions,
    score,
    select_threshold,
)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    import torch
    from build_synthetic import expand_scenarios
    from sentence_transformers import SentenceTransformer

    torch.set_num_threads(4)
    fit_path = ROOT / "calibration-temperature.json"
    selection_path = ROOT / "calibration-selection.json"
    fit, selection = (
        load_partition(fit_path, "temperature"),
        load_partition(selection_path, "selection"),
    )
    prior = expand_scenarios(
        json.loads((ROOT / "synthetic_scenarios.json").read_text())
    )
    prior += load_cases(ROOT / "seed.json") + load_cases(ROOT / "cases.json")
    check_separation([prior, fit, selection])
    args.output.mkdir(parents=True, exist_ok=False)
    head = json.loads((args.model / "head.json").read_text())
    if head["version"] != 1 or head["labels"] != LABELS:
        raise ValueError("unsupported classifier artifact")
    encoder = SentenceTransformer(
        str(args.model / "encoder"),
        device="cpu",
        local_files_only=True,
        trust_remote_code=False,
    )
    coefficients, intercepts = (
        np.asarray(head["coefficients"]),
        np.asarray(head["intercepts"]),
    )
    fit_logits = features(encoder, fit) @ coefficients.T + intercepts
    selection_logits = features(encoder, selection) @ coefficients.T + intercepts
    temperature = fit_temperature(fit_logits, targets(fit))
    selection_values = scaled_probabilities(selection_logits, temperature)
    threshold = select_threshold(head, selection, selection_values)
    raw_threshold = select_threshold(
        head, selection, scaled_probabilities(selection_logits, 1)
    )
    policy = {
        "version": 1,
        "temperature": temperature,
        "threshold": threshold,
        "unscaled_selection_threshold": raw_threshold,
        "temperature_bounds": [0.25, 8.0],
        "temperature_selection": "minimum fit-set negative log likelihood; bounded scalar optimization, max 100 iterations",
        "cutoff_selection": "original grid; maximum selection-set coverage with zero accepted errors",
        "head_sha256": hashlib.sha256(
            (args.model / "head.json").read_bytes()
        ).hexdigest(),
        "encoder_sha256": hashlib.sha256(
            (args.model / "encoder/model.safetensors").read_bytes()
        ).hexdigest(),
        "fit_sha256": hashlib.sha256(fit_path.read_bytes()).hexdigest(),
        "selection_sha256": hashlib.sha256(selection_path.read_bytes()).hexdigest(),
        "labels": head["labels"],
        "fit": calibration_metrics(fit_logits, targets(fit), temperature),
        "selection": calibration_metrics(
            selection_logits, targets(selection), temperature
        ),
        "selection_decisions": score(
            selection, predictions(head, selection_values, threshold)
        ),
    }
    (args.output / "calibration.json").write_text(json.dumps(policy, indent=2) + "\n")
    policy = json.loads((args.output / "calibration.json").read_text())
    audit_path = ROOT / "calibration-audit.json"
    audit = load_partition(audit_path, "audit")
    separation = check_separation([prior, fit, selection, audit])
    audit_logits = features(encoder, audit) @ coefficients.T + intercepts
    raw_values = scaled_probabilities(audit_logits, 1)
    values = scaled_probabilities(audit_logits, policy["temperature"])
    if not np.array_equal(raw_values.argmax(axis=1), values.argmax(axis=1)):
        raise RuntimeError("temperature scaling changed predicted classes")
    report = {
        "policy": policy,
        "separation": separation,
        "audit_sha256": hashlib.sha256(audit_path.read_bytes()).hexdigest(),
        "audit_calibration": calibration_metrics(
            audit_logits, targets(audit), policy["temperature"]
        ),
        "audit_raw": score(audit, predictions(head, raw_values, 0)),
        "audit_original_policy": score(
            audit, predictions(head, raw_values, head["threshold"])
        ),
        "audit_unscaled_reselected_policy": score(
            audit, predictions(head, raw_values, policy["unscaled_selection_threshold"])
        ),
        "audit_calibrated_policy": score(
            audit, predictions(head, values, policy["threshold"])
        ),
        "predictions": {
            case["id"]: {
                "raw": head["labels"][int(row.argmax())],
                "confidence_before": float(raw_row.max()),
                "confidence_after": float(row.max()),
                "selected": label,
            }
            for case, raw_row, row, label in zip(
                audit,
                raw_values,
                values,
                predictions(head, values, policy["threshold"]),
                strict=True,
            )
        },
    }
    (args.output / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    print(
        json.dumps(
            {key: value for key, value in report.items() if key != "predictions"},
            indent=2,
        )
    )


def load_partition(path: Path, split: str) -> list[dict]:
    cases = load_cases(path)
    if (
        json.loads(path.read_text()).get("provenance")
        != "fully_synthetic_assistant_authored"
    ):
        raise ValueError("fully synthetic calibration provenance required")
    if not 5 <= len(cases) <= 1000 or {case["expected"] for case in cases} != set(
        LABELS
    ):
        raise ValueError("expected 5–1000 cases covering all labels")
    if len({case["group"] for case in cases}) != len(cases):
        raise ValueError("one example per calibration family required")
    for case in cases:
        if case["split"] != split:
            raise ValueError("unexpected calibration partition")
        if not case["evidence"] or not any(
            case["evidence"] in message["body"] for message in case["messages"]
        ):
            raise ValueError("evidence must quote a visible message")
    return cases


def check_separation(datasets: list[list[dict]]) -> dict:
    groups, bodies, texts = {}, {}, []
    if sum(map(len, datasets)) > 6000:
        raise ValueError("separation audit limited to 6000 cases")
    for index, cases in enumerate(datasets):
        for case in cases:
            group = case.get("group", case["id"])
            if groups.setdefault(group, index) != index:
                raise ValueError("family crosses partitions")
            for message in case["messages"]:
                normalized = " ".join(re.findall(r"\w+", message["body"].lower()))
                if bodies.setdefault(normalized, index) != index:
                    raise ValueError("message crosses partitions")
            words = re.findall(
                r"\w+", " ".join(m["body"] for m in case["messages"]).lower()
            )
            texts.append((index, set(zip(words, words[1:], words[2:]))))
    maximum = 0.0
    for offset, (left_index, left) in enumerate(texts):
        for right_index, right in texts[offset + 1 :]:
            if left_index == right_index:
                continue
            similarity = len(left & right) / len(left | right) if left | right else 0.0
            maximum = max(maximum, similarity)
            if similarity >= 0.65:
                raise ValueError("near duplicate crosses partitions")
    return {
        "partition_counts": [len(cases) for cases in datasets],
        "cross_partition_trigram_jaccard_max": maximum,
    }


def targets(cases: list[dict]) -> np.ndarray:
    return np.array([LABELS.index(case["expected"]) for case in cases])


def scaled_probabilities(logits: np.ndarray, temperature: float) -> np.ndarray:
    if (
        not np.isfinite(temperature)
        or temperature <= 0
        or not np.isfinite(logits).all()
    ):
        raise ValueError("finite logits and positive finite temperature required")
    scaled = logits / temperature
    return np.exp(scaled - logsumexp(scaled, axis=1, keepdims=True))


def negative_log_likelihood(
    logits: np.ndarray, labels: np.ndarray, temperature: float
) -> float:
    scaled = logits / temperature
    return float(
        np.mean(logsumexp(scaled, axis=1) - scaled[np.arange(len(labels)), labels])
    )


def fit_temperature(logits: np.ndarray, labels: np.ndarray) -> float:
    scaled_probabilities(logits, 1)
    result = minimize_scalar(
        lambda log_temperature: negative_log_likelihood(
            logits, labels, float(np.exp(log_temperature))
        ),
        bounds=(np.log(0.25), np.log(8)),
        method="bounded",
        options={"maxiter": 100, "xatol": 1e-6},
    )
    if not result.success or not np.isfinite(result.fun):
        raise RuntimeError("temperature optimization failed")
    return (
        float(np.exp(result.x))
        if result.fun < negative_log_likelihood(logits, labels, 1)
        else 1.0
    )


def calibration_metrics(
    logits: np.ndarray, labels: np.ndarray, temperature: float
) -> dict:
    report = {}
    for name, value in [("before", 1.0), ("after", temperature)]:
        probabilities = scaled_probabilities(logits, value)
        confidence = probabilities.max(axis=1)
        correct = probabilities.argmax(axis=1) == labels
        bins = []
        ece = 0.0
        for index in range(5):
            mask = (confidence >= index / 5) & (
                (confidence < (index + 1) / 5) if index < 4 else (confidence <= 1.0)
            )
            if not mask.any():
                continue
            accuracy, mean_confidence = (
                float(correct[mask].mean()),
                float(confidence[mask].mean()),
            )
            ece += float(mask.mean()) * abs(accuracy - mean_confidence)
            bins.append(
                {
                    "lower": index / 5,
                    "upper": (index + 1) / 5,
                    "count": int(mask.sum()),
                    "accuracy": accuracy,
                    "confidence": mean_confidence,
                }
            )
        report[name] = {
            "nll": negative_log_likelihood(logits, labels, value),
            "brier_multiclass": float(
                np.mean(
                    np.sum((probabilities - np.eye(len(LABELS))[labels]) ** 2, axis=1)
                )
            ),
            "ece_five_bins": ece,
            "reliability_bins": bins,
        }
    return report


if __name__ == "__main__":
    main()
