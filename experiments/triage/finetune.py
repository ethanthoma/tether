"""Fine-tune MiniLM and its direction-aware head using synthetic train/dev data."""

import argparse
import hashlib
import json
import os
from pathlib import Path

import numpy as np
import torch
from sentence_transformers import SentenceTransformer
from train import (
    BASE,
    REVISION,
    features,
    fit_classifier,
    load_dataset,
    predictions,
    probabilities,
    score,
    select_threshold,
    training_split,
)


def main() -> None:
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--data", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    torch.set_num_threads(4)
    torch.manual_seed(42)
    torch.use_deterministic_algorithms(True)
    cases, policy = load_dataset(args.data)
    train, dev = training_split(cases)
    if len(train) > 2000 or len(dev) > 1000:
        raise ValueError(
            "CPU experiment limited to 2000 training and 1000 development cases"
        )
    args.output.mkdir(parents=True, exist_ok=False)
    encoder = SentenceTransformer(
        BASE, revision=REVISION, device="cpu", trust_remote_code=False
    )
    classifier = fit_classifier(
        features(encoder, train), [case["expected"] for case in train]
    )
    features(encoder, dev)
    labels = classifier.classes_.tolist()
    head = torch.nn.Linear(classifier.coef_.shape[1], len(labels))
    with torch.no_grad():
        head.weight.copy_(torch.from_numpy(classifier.coef_))
        head.bias.copy_(torch.from_numpy(classifier.intercept_))
    targets = torch.tensor([labels.index(case["expected"]) for case in train])
    weights = len(train) / (len(labels) * torch.bincount(targets).float())
    optimizer = torch.optim.AdamW(
        [
            {"params": encoder.parameters(), "lr": 2e-5},
            {"params": head.parameters(), "lr": 1e-3},
        ],
        weight_decay=0.01,
    )
    parameters = list(encoder.parameters()) + list(head.parameters())
    history, best_loss, best_epoch = [], float("inf"), 0
    best_values = None
    for epoch in range(13):
        loss_total = 0.0
        if epoch:
            encoder.train()
            head.train()
            order = torch.randperm(len(train)).tolist()
            for start in range(0, len(order), 16):
                indices = order[start : start + 16]
                optimizer.zero_grad(set_to_none=True)
                matrix = batch_features(encoder, [train[i] for i in indices])
                loss = torch.nn.functional.cross_entropy(
                    head(matrix), targets[indices], weight=weights
                )
                if not torch.isfinite(loss):
                    raise RuntimeError("nonfinite training loss")
                loss.backward()
                torch.nn.utils.clip_grad_norm_(parameters, 1.0, error_if_nonfinite=True)
                optimizer.step()
                loss_total += float(loss.detach()) * len(indices)
        values = evaluate(encoder, head, dev)
        if not np.isfinite(values).all():
            raise RuntimeError("nonfinite development probabilities")
        dev_loss = float(
            -np.log(
                np.maximum(
                    values[
                        np.arange(len(dev)),
                        [labels.index(case["expected"]) for case in dev],
                    ],
                    1e-12,
                )
            ).mean()
        )
        history.append(
            {
                "epoch": epoch,
                "train_loss": loss_total / len(train) if epoch else None,
                "dev_loss": dev_loss,
                "dev_raw": score(dev, predictions({"labels": labels}, values, 0)),
            }
        )
        print(json.dumps(history[-1]), flush=True)
        if dev_loss < best_loss:
            best_loss, best_epoch = dev_loss, epoch
            best_values = values.copy()
            encoder.save_pretrained(args.output / "encoder", safe_serialization=True)
            saved_head = {
                "version": 1,
                "label_policy": policy,
                "base": BASE,
                "revision": REVISION,
                "encoder_frozen": epoch == 0,
                "labels": labels,
                "coefficients": head.weight.detach().tolist(),
                "intercepts": head.bias.detach().tolist(),
            }
            (args.output / "head.json").write_text(
                json.dumps(saved_head, indent=2) + "\n"
            )
    encoder = SentenceTransformer(
        str(args.output / "encoder"), device="cpu", local_files_only=True
    )
    saved_head = json.loads((args.output / "head.json").read_text())
    values = probabilities(saved_head, features(encoder, dev))
    assert best_values is not None
    np.testing.assert_allclose(values, best_values, rtol=1e-4, atol=1e-5)
    saved_head["threshold"] = select_threshold(saved_head, dev, values)
    (args.output / "head.json").write_text(json.dumps(saved_head, indent=2) + "\n")
    report = {
        "label_policy": policy,
        "base": BASE,
        "revision": REVISION,
        "encoder_frozen": best_epoch == 0,
        "data_sha256": hashlib.sha256(args.data.read_bytes()).hexdigest(),
        "train_count": len(train),
        "dev_count": len(dev),
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
        "selected_epoch": best_epoch,
        "threshold": saved_head["threshold"],
        "threshold_selection": "maximum development coverage with zero accepted errors on the original fixed grid",
        "dev_raw": score(dev, predictions(saved_head, values, 0)),
        "dev_selective": score(
            dev, predictions(saved_head, values, saved_head["threshold"])
        ),
        "history": history,
    }
    (args.output / "training.json").write_text(json.dumps(report, indent=2) + "\n")
    print(
        json.dumps(
            {key: value for key, value in report.items() if key != "history"}, indent=2
        )
    )


def batch_features(encoder: SentenceTransformer, cases: list[dict]) -> torch.Tensor:
    if not 1 <= len(cases) <= 16:
        raise ValueError("expected 1–16 cases per batch")
    texts, slots = [], []
    for row, case in enumerate(cases):
        if not 1 <= len(case["messages"]) <= 2:
            raise ValueError("expected one or two messages")
        for position, message in enumerate(case["messages"]):
            texts.append(message["body"])
            slots.append(
                row * 4
                + (2 - len(case["messages"]) + position) * 2
                + int(message["outbound"])
            )
    if (
        max(len(encoder.tokenizer.encode(text)) for text in texts)
        > encoder.max_seq_length
    ):
        raise ValueError("message exceeds encoder token limit")
    encoded = encoder(encoder.preprocess(inputs=texts))["sentence_embedding"]
    encoded = torch.nn.functional.normalize(encoded, dim=1)
    present = torch.cat((encoded, encoded.new_ones((len(encoded), 1))), dim=1)
    matrix = encoded.new_zeros((len(cases) * 4, present.shape[1]))
    return matrix.index_copy(0, torch.tensor(slots), present).reshape(len(cases), -1)


def evaluate(
    encoder: SentenceTransformer, head: torch.nn.Linear, cases: list[dict]
) -> np.ndarray:
    encoder.eval()
    head.eval()
    with torch.no_grad():
        return np.concatenate(
            [
                torch.softmax(
                    head(batch_features(encoder, cases[start : start + 16])), dim=1
                ).numpy()
                for start in range(0, len(cases), 16)
            ]
        )


if __name__ == "__main__":
    main()
