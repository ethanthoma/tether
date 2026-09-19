"""Fine-tune the NLI encoder over joint synthetic threads and direction bits."""

import argparse
import hashlib
import json
import os
from pathlib import Path

import numpy as np
import torch
from calibrate import fit_temperature, negative_log_likelihood, scaled_probabilities
from sentence_transformers import SentenceTransformer
from train import (
    BASE,
    CHECKPOINT_SELECTION,
    CONTEXT_TOKENS_MAX,
    FEATURE_LAYOUT,
    HEAD_VERSION,
    REVISION,
    TEMPERATURE_BOUNDS,
    TEMPERATURE_SELECTION,
    THRESHOLD_SELECTION,
    features,
    fit_classifier,
    load_base_encoder,
    load_dataset,
    predictions,
    probabilities,
    score,
    select_thresholds,
    thread_inputs,
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
    if policy != "reply-triage-v2" or len(train) > 3000 or len(dev) > 1000:
        raise ValueError("expected bounded v2 training/development data")
    encoder = load_base_encoder()
    position_capacity = encoder[0].auto_model.config.max_position_embeddings
    train_vectors = features(encoder, train)
    features(encoder, dev)
    with torch.no_grad():
        np.testing.assert_allclose(
            batch_features(encoder, train[:16]).numpy(),
            train_vectors[:16],
            rtol=1e-4,
            atol=1e-5,
        )
    classifier = fit_classifier(train_vectors, [case["expected"] for case in train])
    labels = classifier.classes_.tolist()
    dev_targets = np.array([labels.index(case["expected"]) for case in dev])
    head = torch.nn.Linear(388, len(labels))
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
    args.output.mkdir(parents=True, exist_ok=False)
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
        logits = evaluate(encoder, head, dev)
        if not np.isfinite(logits).all():
            raise RuntimeError("nonfinite development logits")
        temperature = fit_temperature(logits, dev_targets)
        dev_loss = negative_log_likelihood(logits, dev_targets, 1.0)
        calibrated_dev_loss = negative_log_likelihood(logits, dev_targets, temperature)
        values = scaled_probabilities(logits, temperature)
        history.append(
            {
                "epoch": epoch,
                "train_loss": loss_total / len(train) if epoch else None,
                "dev_loss": dev_loss,
                "calibrated_dev_loss": calibrated_dev_loss,
                "temperature": temperature,
                "dev_raw": score(dev, predictions({"labels": labels}, values)),
            }
        )
        print(json.dumps(history[-1]), flush=True)
        if calibrated_dev_loss < best_loss:
            best_loss, best_epoch = calibrated_dev_loss, epoch
            best_values = values.copy()
            encoder.save_pretrained(args.output / "encoder", safe_serialization=True)
            unscaled_head = {
                "version": HEAD_VERSION,
                "feature_layout": FEATURE_LAYOUT,
                "label_policy": policy,
                "base": BASE,
                "revision": REVISION,
                "encoder_frozen": epoch == 0,
                "labels": labels,
                "coefficients": head.weight.detach().tolist(),
                "intercepts": head.bias.detach().tolist(),
            }
            unscaled_content = (json.dumps(unscaled_head, indent=2) + "\n").encode()
            (args.output / "unscaled-head.json").write_bytes(unscaled_content)
            saved_head = fold_temperature(
                unscaled_head, temperature, hashlib.sha256(unscaled_content).hexdigest()
            )
            (args.output / "head.json").write_text(
                json.dumps(saved_head, indent=2) + "\n"
            )
    encoder = SentenceTransformer(
        str(args.output / "encoder"),
        device="cpu",
        local_files_only=True,
        trust_remote_code=False,
    )
    if encoder.max_seq_length != CONTEXT_TOKENS_MAX:
        raise ValueError("saved encoder token limit changed")
    saved_head = json.loads((args.output / "head.json").read_text())
    unscaled_content = (args.output / "unscaled-head.json").read_bytes()
    if (
        hashlib.sha256(unscaled_content).hexdigest()
        != saved_head["unscaled_head_sha256"]
    ):
        raise ValueError("unscaled checkpoint provenance mismatch")
    unscaled_head = json.loads(unscaled_content)
    matrix = features(encoder, dev)
    values = probabilities(saved_head, matrix)
    raw_values = probabilities(unscaled_head, matrix)
    if predictions(saved_head, values) != predictions(unscaled_head, raw_values):
        raise ValueError("temperature scaling changed predicted classes")
    assert best_values is not None
    np.testing.assert_allclose(values, best_values, rtol=1e-4, atol=1e-5)
    saved_head["thresholds"] = select_thresholds(saved_head, dev, values)
    (args.output / "head.json").write_text(json.dumps(saved_head, indent=2) + "\n")
    report = {
        "label_policy": policy,
        "base": BASE,
        "revision": REVISION,
        "feature_layout": FEATURE_LAYOUT,
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
        "checkpoint_selection": CHECKPOINT_SELECTION,
        "selected_epoch": best_epoch,
        "temperature": saved_head["temperature"],
        "temperature_bounds": TEMPERATURE_BOUNDS,
        "temperature_selection": TEMPERATURE_SELECTION,
        "unscaled_head_sha256": saved_head["unscaled_head_sha256"],
        "max_seq_length": CONTEXT_TOKENS_MAX,
        "base_position_capacity": position_capacity,
        "thresholds": saved_head["thresholds"],
        "threshold_selection": THRESHOLD_SELECTION,
        "dev_raw": score(dev, predictions(saved_head, values)),
        "dev_selective": score(
            dev, predictions(saved_head, values, saved_head["thresholds"])
        ),
        "history": history,
    }
    (args.output / "training.json").write_text(json.dumps(report, indent=2) + "\n")
    print(
        json.dumps(
            {key: value for key, value in report.items() if key != "history"}, indent=2
        ),
        flush=True,
    )


def fold_temperature(
    unscaled_head: dict, temperature: float, source_sha256: str
) -> dict:
    if "temperature" in unscaled_head or "unscaled_head_sha256" in unscaled_head:
        raise ValueError("head has already been temperature-scaled")
    if (
        type(temperature) not in (int, float)
        or not np.isfinite(temperature)
        or not TEMPERATURE_BOUNDS[0] <= temperature <= TEMPERATURE_BOUNDS[1]
    ):
        raise ValueError("temperature outside the frozen bounds")
    if (
        not isinstance(source_sha256, str)
        or len(source_sha256) != 64
        or any(character not in "0123456789abcdef" for character in source_sha256)
    ):
        raise ValueError("invalid unscaled head hash")
    coefficients = np.asarray(unscaled_head["coefficients"], dtype=np.float64)
    intercepts = np.asarray(unscaled_head["intercepts"], dtype=np.float64)
    if not np.isfinite(coefficients).all() or not np.isfinite(intercepts).all():
        raise ValueError("nonfinite classifier weights")
    return {
        **unscaled_head,
        "coefficients": (coefficients / temperature).tolist(),
        "intercepts": (intercepts / temperature).tolist(),
        "temperature": temperature,
        "temperature_bounds": TEMPERATURE_BOUNDS.copy(),
        "temperature_selection": TEMPERATURE_SELECTION,
        "unscaled_head_sha256": source_sha256,
    }


def batch_features(encoder: SentenceTransformer, cases: list[dict]) -> torch.Tensor:
    if not 1 <= len(cases) <= 16:
        raise ValueError("expected 1–16 cases per batch")
    texts, present = thread_inputs(encoder, cases)
    encoded = encoder(encoder.preprocess(inputs=texts))["sentence_embedding"]
    if encoded.shape != (len(cases), 384):
        raise ValueError("unexpected encoder dimensions")
    encoded = torch.nn.functional.normalize(encoded, dim=1)
    return torch.cat((encoded, torch.from_numpy(present)), dim=1)


def evaluate(
    encoder: SentenceTransformer, head: torch.nn.Linear, cases: list[dict]
) -> np.ndarray:
    encoder.eval()
    head.eval()
    matrix = features(encoder, cases)
    coefficients = np.asarray(head.weight.detach().tolist(), dtype=np.float64)
    intercepts = np.asarray(head.bias.detach().tolist(), dtype=np.float64)
    return matrix @ coefficients.T + intercepts


if __name__ == "__main__":
    main()
