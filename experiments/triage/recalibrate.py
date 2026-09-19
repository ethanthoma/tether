"""Calibrate an archived scalar-head candidate using development labels only."""

import argparse
import hashlib
import json
import os
import shutil
from pathlib import Path

import numpy as np
from shadow import artifact_sha256, load_artifact, validate_head
from train import (
    HEAD_VERSION,
    LABELS,
    THRESHOLD_GRID,
    THRESHOLD_SELECTION,
    features,
    label_policy,
    load_dataset,
    predictions,
    probabilities,
    score,
    select_thresholds,
    training_split,
)


def main() -> None:
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", required=True, type=Path)
    parser.add_argument("--data", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    recalibrate(args.model, args.data, args.output)


def weights_sha256(head: dict) -> str:
    weights = {field: head[field] for field in ("labels", "coefficients", "intercepts")}
    return hashlib.sha256(
        json.dumps(
            weights, sort_keys=True, separators=(",", ":"), allow_nan=False
        ).encode()
    ).hexdigest()


def recalibrate(model: Path, data: Path, output: Path) -> dict:
    model = model.resolve(strict=True)
    cases, policy = load_dataset(data)
    train, dev = training_split(cases)
    if policy != "reply-triage-v2" or len(train) > 2000 or len(dev) > 1000:
        raise ValueError("expected bounded v2 train/development data")
    source = {}
    for name, size_max in (
        ("head.json", 1024 * 1024),
        ("training.json", 4 * 1024 * 1024),
    ):
        path = model / name
        if path.is_symlink() or not path.is_file() or path.stat().st_size > size_max:
            raise ValueError("invalid source metadata")
        source[name] = path.read_bytes()
    original_head = json.loads(source["head.json"])
    original_report = json.loads(source["training.json"])
    threshold = original_head.get("threshold")
    if (
        type(original_head.get("version")) is not int
        or original_head["version"] != 1
        or "thresholds" in original_head
        or type(threshold) not in (int, float)
        or not np.isfinite(threshold)
        or not 0 <= threshold <= 1
    ):
        raise ValueError("expected archived scalar-head candidate")
    if (
        label_policy(original_head) != policy
        or original_report.get("label_policy") != policy
    ):
        raise ValueError("source policy mismatch")
    data_hash = hashlib.sha256(data.read_bytes()).hexdigest()
    if (
        original_report.get("data_sha256") != data_hash
        or original_report.get("train_count") != len(train)
        or original_report.get("dev_count") != len(dev)
    ):
        raise ValueError("source training data mismatch")
    head = {key: value for key, value in original_head.items() if key != "threshold"}
    head.update(version=HEAD_VERSION, thresholds=dict.fromkeys(LABELS, 1.0))
    validate_head(head)
    source_identity = artifact_sha256(model)
    encoder_hash = artifact_sha256(model, include_head=False)
    weights_hash = weights_sha256(original_head)

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
    values = probabilities(head, features(encoder, dev))
    head["thresholds"] = select_thresholds(head, dev, values)
    report = {
        key: value for key, value in original_report.items() if key != "threshold"
    }
    report.update(
        thresholds=head["thresholds"],
        threshold_selection=THRESHOLD_SELECTION,
        dev_raw=score(dev, predictions(head, values)),
        dev_selective=score(dev, predictions(head, values, head["thresholds"])),
    )
    calibration = {
        "version": 1,
        "label_policy": policy,
        "method": THRESHOLD_SELECTION,
        "data_sha256": data_hash,
        "dev_count": len(dev),
        "threshold_grid": THRESHOLD_GRID,
        "thresholds": head["thresholds"],
        "source_head_sha256": hashlib.sha256(source["head.json"]).hexdigest(),
        "source_training_report_sha256": hashlib.sha256(
            source["training.json"]
        ).hexdigest(),
        "source_encoder_sha256": encoder_hash,
        "source_weights_sha256": weights_hash,
        "heldout_observed": False,
    }
    if (
        artifact_sha256(model) != source_identity
        or (model / "training.json").read_bytes() != source["training.json"]
    ):
        raise ValueError("source changed during calibration")
    output.mkdir(parents=True, exist_ok=False)
    shutil.copytree(model / "encoder", output / "encoder")
    if (
        artifact_sha256(output, include_head=False) != encoder_hash
        or weights_sha256(head) != weights_hash
    ):
        raise ValueError("calibration changed encoder or classifier weights")
    for field in ("base", "revision", "encoder_frozen"):
        if head[field] != original_head[field]:
            raise ValueError("calibration changed model provenance")
    for name, document in (
        ("head.json", head),
        ("training.json", report),
        ("calibration.json", calibration),
    ):
        (output / name).write_text(
            json.dumps(document, indent=2, allow_nan=False) + "\n"
        )
    (output / "source-head.json").write_bytes(source["head.json"])
    (output / "source-training.json").write_bytes(source["training.json"])
    load_artifact(output)
    print(
        json.dumps(
            {
                "thresholds": head["thresholds"],
                "dev_selective": report["dev_selective"],
            },
            indent=2,
        )
    )
    return calibration


if __name__ == "__main__":
    main()
