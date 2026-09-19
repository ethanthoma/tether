"""Measure conditional label codelength on unseen training families only."""

import argparse
import hashlib
import json
from pathlib import Path

import numpy as np
from train import (
    BASE,
    LABELS,
    REVISION,
    features,
    fit_classifier,
    load_cases,
    training_split,
)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--data", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    import torch
    from sentence_transformers import SentenceTransformer

    torch.set_num_threads(4)
    train, _ = training_split(load_cases(args.data))
    encoder = SentenceTransformer(
        BASE,
        revision=REVISION,
        device="cpu",
        local_files_only=True,
        trust_remote_code=False,
    )
    matrix = features(encoder, train)
    labels = np.array([LABELS.index(case["expected"]) for case in train])
    groups = [case["group"] for case in train]
    shuffled = np.random.default_rng(2026).permutation(labels)
    runs = [prequential(matrix, labels, groups, seed) for seed in (41, 42, 43)]
    controls = [prequential(matrix, shuffled, groups, seed) for seed in (41, 42, 43)]
    report = {
        "base": BASE,
        "revision": REVISION,
        "interpretation": "conditional prequential label code; not Kolmogorov complexity or a quality certificate",
        "side_information": "frozen encoder, messages/directions, family boundaries, order seeds, learner and coding protocol",
        "data_sha256": hashlib.sha256(args.data.read_bytes()).hexdigest(),
        "train_cases": len(train),
        "train_families": len(set(groups)),
        "uniform_bits_per_case": float(np.log2(len(LABELS))),
        "learner_bits_per_case_mean": float(
            np.mean([r["learner_bits"] for r in runs]) / len(train)
        ),
        "prior_bits_per_case_mean": float(
            np.mean([r["prior_bits"] for r in runs]) / len(train)
        ),
        "shuffled_bits_per_case_mean": float(
            np.mean([r["learner_bits"] for r in controls]) / len(train)
        ),
        "runs": runs,
        "shuffled_label_runs": controls,
    }
    with args.output.open("x") as output:
        json.dump(report, output, indent=2)
        output.write("\n")
    print(
        json.dumps(
            {
                key: value
                for key, value in report.items()
                if key not in {"runs", "shuffled_label_runs"}
            },
            indent=2,
        )
    )


def prequential(
    matrix: np.ndarray, labels: np.ndarray, groups: list[str], seed: int
) -> dict:
    if (
        len(matrix) != len(labels)
        or len(labels) != len(groups)
        or not 1 <= len(labels) <= 6000
    ):
        raise ValueError("expected matching, bounded examples, labels, and families")
    if not np.isin(labels, np.arange(len(LABELS))).all():
        raise ValueError("invalid label index")
    order = sorted(set(groups))
    np.random.default_rng(seed).shuffle(order)
    boundaries = sorted({min(len(order), count) for count in (8, 16, 32, len(order))})
    learner_bits, prior_bits, previous = 0.0, 0.0, 0
    blocks = []
    group_array = np.array(groups)
    for end in boundaries:
        train_mask = np.isin(group_array, order[:previous])
        test_mask = np.isin(group_array, order[previous:end])
        targets = labels[test_mask]
        prior = np.bincount(labels[train_mask], minlength=len(LABELS)) + 1.0
        prior /= prior.sum()
        values = np.tile(prior, (len(targets), 1))
        if len(set(labels[train_mask])) >= 2:
            classifier = fit_classifier(
                matrix[train_mask], [LABELS[i] for i in labels[train_mask]]
            )
            values = np.full((len(targets), len(LABELS)), 1e-6)
            indices = [LABELS.index(label) for label in classifier.classes_]
            values[:, indices] += classifier.predict_proba(matrix[test_mask])
            values /= values.sum(axis=1, keepdims=True)
        bits = float(-np.log2(values[np.arange(len(targets)), targets]).sum())
        learner_bits += bits
        prior_bits += float(-np.log2(prior[targets]).sum())
        blocks.append(
            {
                "train_families": previous,
                "encoded_families": end - previous,
                "encoded_cases": len(targets),
                "learner_bits": bits,
            }
        )
        previous = end
    return {
        "seed": seed,
        "learner_bits": learner_bits,
        "prior_bits": prior_bits,
        "blocks": blocks,
    }


if __name__ == "__main__":
    main()
