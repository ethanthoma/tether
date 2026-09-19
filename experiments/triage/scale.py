"""Export nested synthetic training sets with shared development and fresh test data."""

import argparse
import hashlib
import json
import random
from pathlib import Path

from build_synthetic import audit_cases, expand_scenarios
from calibrate import check_separation
from train import ROOT, load_cases, training_split


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    original = expand_scenarios(
        json.loads((ROOT / "synthetic_scenarios.json").read_text())
    )
    added = expand_scenarios(json.loads((ROOT / "scale_scenarios.json").read_text()))
    previous = list(original)
    for filename in (
        "seed.json",
        "cases.json",
        "calibration-temperature.json",
        "calibration-selection.json",
        "calibration-audit.json",
    ):
        previous.extend(load_cases(ROOT / filename))
    separation = check_separation([previous, added])
    combined = [case for case in original if case["split"] != "test"] + added
    audit = audit_cases(combined)
    datasets = nested_datasets(original, added)
    datasets["holdout"] = [case for case in added if case["split"] == "test"]
    args.output.mkdir(parents=True, exist_ok=False)
    manifest = {
        "seed": 42,
        "audit": audit,
        "historical_separation": separation,
        "datasets": {},
    }
    for name, cases in datasets.items():
        payload = {
            "version": 1,
            "provenance": "fully_synthetic_assistant_authored",
            "review_status": "single_author_self_reviewed_not_independently_validated",
            "cases": cases,
        }
        path = args.output / f"{name}.json"
        path.write_text(json.dumps(payload, indent=2) + "\n")
        load_cases(path)
        manifest["datasets"][name] = {
            "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
            "splits": {
                split: {
                    "cases": sum(case["split"] == split for case in cases),
                    "families": sorted(
                        {case["group"] for case in cases if case["split"] == split}
                    ),
                }
                for split in sorted({case["split"] for case in cases})
            },
        }
    (args.output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(json.dumps(audit, indent=2))


def nested_datasets(original: list[dict], added: list[dict]) -> dict[str, list[dict]]:
    groups = sorted({case["group"] for case in added if case["split"] == "train"})
    if len(groups) != 100:
        raise ValueError("scaling experiment requires 100 new training families")
    random.Random(42).shuffle(groups)
    baseline = [case for case in original if case["split"] == "train"]
    dev = [case for case in original + added if case["split"] == "dev"]
    datasets = {}
    for name, count in (("small", 0), ("medium", 50), ("full", 100)):
        selected = set(groups[:count])
        cases = baseline + [case for case in added if case["group"] in selected] + dev
        training_split(cases)
        datasets[name] = cases
    return datasets


if __name__ == "__main__":
    main()
