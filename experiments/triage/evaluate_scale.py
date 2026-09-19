"""Freeze development-based model selection, then report the synthetic learning curve."""

import argparse
import hashlib
import json
from pathlib import Path

import torch
from sentence_transformers import SentenceTransformer
from train import features, load_cases, predictions, probabilities, score


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run", type=Path, required=True)
    args = parser.parse_args()
    torch.set_num_threads(4)
    manifest = json.loads((args.run / "manifest.json").read_text())
    runs = {}
    for name in ("small", "medium", "full"):
        model = args.run / f"{name}-model"
        training = json.loads((model / "training.json").read_text())
        digest = hashlib.sha256((args.run / f"{name}.json").read_bytes()).hexdigest()
        if (
            digest != training["data_sha256"]
            or digest != manifest["datasets"][name]["sha256"]
        ):
            raise ValueError("training dataset digest mismatch")
        runs[name] = {
            "training": training,
            "train_families": len(
                manifest["datasets"][name]["splits"]["train"]["families"]
            ),
            "selected_dev_loss": min(row["dev_loss"] for row in training["history"]),
            "head_sha256": hashlib.sha256(
                (model / "head.json").read_bytes()
            ).hexdigest(),
            "encoder_sha256": hashlib.sha256(
                (model / "encoder/model.safetensors").read_bytes()
            ).hexdigest(),
        }
    selected = min(runs, key=lambda name: runs[name]["selected_dev_loss"])
    selection = {
        "rule": "lowest selected development cross entropy; ties prefer smaller training set",
        "candidate": selected,
        "development_losses": {
            name: run["selected_dev_loss"] for name, run in runs.items()
        },
    }
    with (args.run / "selection.json").open("x") as stream:
        stream.write(json.dumps(selection, indent=2) + "\n")
    holdout = args.run / "holdout.json"
    if (
        hashlib.sha256(holdout.read_bytes()).hexdigest()
        != manifest["datasets"]["holdout"]["sha256"]
    ):
        raise ValueError("holdout digest mismatch")
    cases = load_cases(holdout)
    for name, run in runs.items():
        model = args.run / f"{name}-model"
        head = json.loads((model / "head.json").read_text())
        encoder = SentenceTransformer(
            str(model / "encoder"), device="cpu", local_files_only=True
        )
        values = probabilities(head, features(encoder, cases))
        raw = predictions(head, values, 0)
        selective = predictions(head, values, head["threshold"])
        run["test_raw"] = score(cases, raw)
        run["test_selective"] = score(cases, selective)
        run["predictions"] = {
            case["id"]: {"raw": before, "selective": after}
            for case, before, after in zip(cases, raw, selective, strict=True)
        }
    report = {"selection": selection, "manifest": manifest, "runs": runs}
    with (args.run / "report.json").open("x") as stream:
        stream.write(json.dumps(report, indent=2) + "\n")
    print(
        json.dumps(
            {
                name: {
                    key: value for key, value in run.items() if key.startswith("test_")
                }
                for name, run in runs.items()
            },
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
