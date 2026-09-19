import copy
import hashlib
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import numpy as np
from calibrate import fit_temperature, negative_log_likelihood
from release import (
    main,
    read_unscaled_head,
    release_metrics,
    validate_calibration,
    validate_development,
    validate_reviewed_source,
    validate_test,
    validate_training,
)
from train import (
    BASE,
    CHECKPOINT_SELECTION,
    CONTEXT_TOKENS_MAX,
    FEATURE_LAYOUT,
    HEAD_VERSION,
    LABELS,
    REVISION,
    TEMPERATURE_BOUNDS,
    TEMPERATURE_SELECTION,
    THRESHOLD_SELECTION,
)


class ReleaseTests(unittest.TestCase):
    def setUp(self) -> None:
        self.cases = [
            {
                "id": str(index),
                "group": f"family_{index // 4}",
                "split": "test",
                "expected": LABELS[index % 5],
            }
            for index in range(100)
        ]
        self.correct = [case["expected"] for case in self.cases]

    def test_gate_requires_safe_coverage_and_both_actionable_classes(self) -> None:
        self.assertTrue(
            release_metrics(self.cases, self.correct, self.correct)["approved"]
        )
        self.assertFalse(
            release_metrics(self.cases, ["abstain"] * 100, self.correct)["approved"]
        )
        unsafe = self.correct.copy()
        unsafe[0] = "noise" if unsafe[0] != "noise" else "fyi"
        report = release_metrics(self.cases, unsafe, self.correct)
        self.assertFalse(report["approved"])
        self.assertEqual(report["accepted_errors"], 1)
        passive = [
            label if label in {"fyi", "noise"} else "abstain" for label in self.correct
        ]
        report = release_metrics(self.cases, passive, self.correct)
        self.assertGreaterEqual(report["coverage"], 0.25)
        self.assertFalse(report["approved"])

    def test_coverage_boundary(self) -> None:
        available = [
            index for index, label in enumerate(self.correct) if label != "abstain"
        ]
        for count, approved in ((24, False), (25, True)):
            selected = ["abstain"] * 100
            for index in available[:count]:
                selected[index] = self.correct[index]
            self.assertEqual(
                release_metrics(self.cases, selected, self.correct)["approved"],
                approved,
            )

    def test_development_readiness_preserves_holdout_on_unusable_candidate(
        self,
    ) -> None:
        cases = [{**case, "split": "dev"} for case in self.cases]
        report = validate_development(cases, self.correct)
        self.assertEqual(report["accepted_errors"], 0)
        self.assertEqual(report["actionable_correct"]["needs_reply"], 20)
        for selected in (
            ["abstain"] * 100,
            [
                label if label in {"fyi", "noise"} else "abstain"
                for label in self.correct
            ],
            ["needs_reply", *self.correct[1:]],
            self.correct[:-1],
        ):
            with self.subTest(selected=selected[:3]), self.assertRaises(ValueError):
                validate_development(cases, selected)
        with self.assertRaises(ValueError):
            validate_development(self.cases, self.correct)

    def test_development_readiness_coverage_boundary(self) -> None:
        cases = [{**case, "split": "dev"} for case in self.cases]
        available = [
            index for index, label in enumerate(self.correct) if label != "abstain"
        ]
        selected = ["abstain"] * 100
        for index in available[:24]:
            selected[index] = self.correct[index]
        with self.assertRaises(ValueError):
            validate_development(cases, selected)
        selected[available[24]] = self.correct[available[24]]
        self.assertEqual(validate_development(cases, selected)["coverage"], 0.25)

    def test_test_contract_and_prediction_count_fail_closed(self) -> None:
        for cases in (
            self.cases[:79],
            [{**case, "split": "train"} for case in self.cases],
            [{**case, "group": "one"} for case in self.cases],
            [{**case, "expected": "fyi"} for case in self.cases],
        ):
            with self.subTest(cases=len(cases)), self.assertRaises(ValueError):
                validate_test(cases)
        with self.assertRaises(ValueError):
            release_metrics(self.cases, self.correct[:-1], self.correct)
        with self.assertRaises(ValueError):
            release_metrics(self.cases, ["unknown"] * 100, self.correct)

    def test_raw_accuracy_counts_correct_explicit_abstentions(self) -> None:
        report = release_metrics(self.cases, self.correct, self.correct)
        self.assertEqual(report["raw_accuracy"], 1.0)
        self.assertEqual(report["coverage"], 0.8)
        raw = ["abstain"] * 100
        report = release_metrics(self.cases, self.correct, raw)
        self.assertEqual(report["raw_accuracy"], 0.2)

    def training_fixture(self) -> tuple[dict, dict, bytes, list[dict], np.ndarray]:
        cases = [{"split": "dev", "expected": label} for label in LABELS] * 4
        matrix = np.zeros((20, 388), dtype=np.float32)
        for index in range(20):
            matrix[index, index % 5] = 0.5
        coefficients = np.zeros((5, 388))
        coefficients[:, :5] = np.eye(5)
        intercepts = np.arange(5) * 0.01
        logits = matrix @ coefficients.T + intercepts
        targets = np.array([LABELS.index(case["expected"]) for case in cases])
        temperature = fit_temperature(logits, targets)
        raw_loss = negative_log_likelihood(logits, targets, 1.0)
        calibrated_loss = negative_log_likelihood(logits, targets, temperature)
        unscaled = {
            "version": HEAD_VERSION,
            "label_policy": "reply-triage-v2",
            "labels": LABELS,
            "coefficients": coefficients.tolist(),
            "intercepts": intercepts.tolist(),
            "feature_layout": FEATURE_LAYOUT,
            "base": BASE,
            "revision": REVISION,
            "encoder_frozen": False,
        }
        unscaled_bytes = json.dumps(unscaled).encode()
        head = {
            **unscaled,
            "coefficients": (coefficients / temperature).tolist(),
            "intercepts": (intercepts / temperature).tolist(),
            "temperature": temperature,
            "temperature_bounds": TEMPERATURE_BOUNDS,
            "temperature_selection": TEMPERATURE_SELECTION,
            "unscaled_head_sha256": hashlib.sha256(unscaled_bytes).hexdigest(),
            "feature_layout": FEATURE_LAYOUT,
            "base": BASE,
            "revision": REVISION,
            "thresholds": {
                label: 1.0 if label == "abstain" else 0.8 for label in LABELS
            },
            "encoder_frozen": False,
        }
        report = {
            "feature_layout": FEATURE_LAYOUT,
            "max_seq_length": CONTEXT_TOKENS_MAX,
            "base_position_capacity": CONTEXT_TOKENS_MAX,
            "label_policy": "reply-triage-v2",
            "base": BASE,
            "revision": REVISION,
            "data_sha256": "input-hash",
            "train_count": 100,
            "dev_count": 20,
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
            "temperature": temperature,
            "temperature_bounds": TEMPERATURE_BOUNDS,
            "temperature_selection": TEMPERATURE_SELECTION,
            "unscaled_head_sha256": head["unscaled_head_sha256"],
            "threshold_selection": THRESHOLD_SELECTION,
            "thresholds": head["thresholds"],
            "selected_epoch": 12,
            "encoder_frozen": False,
            "history": [
                {
                    "epoch": i,
                    "dev_loss": raw_loss + 12 - i,
                    "calibrated_dev_loss": calibrated_loss + 12 - i,
                    "temperature": temperature,
                }
                for i in range(13)
            ],
        }
        return report, head, unscaled_bytes, cases, matrix

    def test_training_provenance_rejects_recipe_input_and_selection_changes(
        self,
    ) -> None:
        report, head, _, _, _ = self.training_fixture()
        validate_training(report, head, "input-hash", 100, 20)
        for field, value in (
            ("seed", 43),
            ("checkpoint_selection", "raw loss"),
            ("temperature_bounds", [0.1, 10.0]),
            ("temperature_selection", "test fit"),
            ("temperature", True),
            ("temperature", 9.0),
            ("unscaled_head_sha256", "invalid"),
            ("feature_layout", "old-layout"),
            ("max_seq_length", 256),
            ("base_position_capacity", 256),
            ("epochs", 11),
            ("batch_size", 32),
            ("encoder_learning_rate", 1e-5),
            ("head_learning_rate", 0.01),
            ("weight_decay", 0.0),
            ("gradient_norm_max", 2.0),
            ("loss", "unweighted"),
            ("head_initialization", "random"),
            ("base", "different"),
            ("revision", "different"),
            ("label_policy", "synthetic-obligations-v1"),
            ("train_count", 101),
            ("dev_count", 21),
            ("data_sha256", "changed"),
            ("thresholds", dict.fromkeys(LABELS, 1.0)),
            ("selected_epoch", 11),
            ("selected_epoch", True),
            ("encoder_frozen", True),
            ("history", report["history"][1:]),
        ):
            with self.subTest(field=field), self.assertRaises(ValueError):
                validate_training({**report, field: value}, head, "input-hash", 100, 20)
        for field in ("dev_loss", "calibrated_dev_loss", "temperature"):
            for value in (float("nan"), float("inf"), -1, True):
                changed = copy.deepcopy(report)
                changed["history"][0][field] = value
                with (
                    self.subTest(field=field, value=value),
                    self.assertRaises(ValueError),
                ):
                    validate_training(changed, head, "input-hash", 100, 20)
        tied = copy.deepcopy(report)
        tied["history"][0]["calibrated_dev_loss"] = tied["history"][12][
            "calibrated_dev_loss"
        ]
        with self.assertRaises(ValueError):
            validate_training(tied, head, "input-hash", 100, 20)
        tied["selected_epoch"] = 0
        tied["encoder_frozen"] = True
        validate_training(tied, {**head, "encoder_frozen": True}, "input-hash", 100, 20)

    def test_calibration_verifies_actual_logits_and_single_parameter_scaling(
        self,
    ) -> None:
        report, head, unscaled_bytes, cases, matrix = self.training_fixture()
        values = validate_calibration(report, head, unscaled_bytes, cases, matrix)
        self.assertEqual(values.shape, (20, 5))
        self.assertTrue(np.isfinite(values).all())
        self.assertNotEqual(head["temperature"], 1.0)
        for key in ("coefficients", "intercepts"):
            changed = copy.deepcopy(head)
            changed[key] = (np.asarray(changed[key]) / head["temperature"]).tolist()
            with (
                self.subTest(key=key),
                self.assertRaisesRegex(ValueError, "scaled exactly once"),
            ):
                validate_calibration(report, changed, unscaled_bytes, cases, matrix)
        changed = copy.deepcopy(head)
        changed["intercepts"][0] += 0.01
        with self.assertRaisesRegex(ValueError, "scaled exactly once"):
            validate_calibration(report, changed, unscaled_bytes, cases, matrix)
        for source in ("head", "report"):
            for temperature in (True, float("nan"), 0.5):
                changed_head, changed_report = (
                    copy.deepcopy(head),
                    copy.deepcopy(report),
                )
                (changed_head if source == "head" else changed_report)[
                    "temperature"
                ] = temperature
                with (
                    self.subTest(source=source, temperature=temperature),
                    self.assertRaises(ValueError),
                ):
                    validate_calibration(
                        changed_report, changed_head, unscaled_bytes, cases, matrix
                    )
        for key in ("dev_loss", "calibrated_dev_loss"):
            changed = copy.deepcopy(report)
            changed["history"][12][key] += 0.01
            with (
                self.subTest(key=key),
                self.assertRaisesRegex(ValueError, "actual logits"),
            ):
                validate_calibration(changed, head, unscaled_bytes, cases, matrix)

    def test_calibration_rejects_malformed_or_unbound_unscaled_head(self) -> None:
        report, head, unscaled_bytes, cases, matrix = self.training_fixture()
        with self.assertRaisesRegex(ValueError, "hash"):
            validate_calibration(report, head, unscaled_bytes + b" ", cases, matrix)
        for key, value in (
            ("temperature", 1.0),
            ("labels", list(reversed(LABELS))),
            ("base", "wrong-base"),
            ("coefficients", [[0.0]]),
            ("intercepts", [float("nan")] * 5),
        ):
            unscaled = json.loads(unscaled_bytes)
            unscaled[key] = value
            changed_bytes = json.dumps(unscaled).encode()
            digest = hashlib.sha256(changed_bytes).hexdigest()
            with self.subTest(key=key), self.assertRaises(ValueError):
                validate_calibration(
                    {**report, "unscaled_head_sha256": digest},
                    {**head, "unscaled_head_sha256": digest},
                    changed_bytes,
                    cases,
                    matrix,
                )
        with self.assertRaisesRegex(ValueError, "development-only"):
            validate_calibration(
                report,
                head,
                unscaled_bytes,
                [{**case, "split": "test"} for case in cases],
                matrix,
            )

    def test_unscaled_head_read_rejects_symlinks_and_oversize_files(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "unscaled-head.json"
            path.write_bytes(b"{}")
            self.assertEqual(read_unscaled_head(path), b"{}")
            link = Path(directory) / "linked.json"
            link.symlink_to(path)
            with self.assertRaises(OSError):
                read_unscaled_head(link)
            path.write_bytes(b"x" * (1024 * 1024 + 1))
            with self.assertRaisesRegex(ValueError, "regular file"):
                read_unscaled_head(path)
            path.write_bytes(b"")
            with self.assertRaises(ValueError):
                read_unscaled_head(path)

    def test_calibration_failure_prevents_held_out_feature_extraction(self) -> None:
        report, head, unscaled_bytes, dev, matrix = self.training_fixture()
        head["intercepts"][0] += 0.01
        source = {
            "provenance": "fully_synthetic_assistant_authored",
            "review_status": "blind_reviewer_agreed_independence_self_attested",
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            training = root / "training-data.json"
            training.write_text(json.dumps(source))
            data = root / "test-data.json"
            data.write_text(json.dumps(source))
            report["data_sha256"] = hashlib.sha256(training.read_bytes()).hexdigest()
            (root / "training.json").write_text(json.dumps(report))
            (root / "unscaled-head.json").write_bytes(unscaled_bytes)
            output = root / "release.json"
            with (
                patch(
                    "sys.argv",
                    [
                        "release.py",
                        "--model",
                        str(root),
                        "--training-data",
                        str(training),
                        "--data",
                        str(data),
                        "--plan",
                        str(root / "plan.md"),
                        "--output",
                        str(output),
                    ],
                ),
                patch(
                    "release.load_dataset",
                    side_effect=[
                        (self.cases, "reply-triage-v2"),
                        ([], "reply-triage-v2"),
                    ],
                ),
                patch("release.training_split", return_value=([{}] * 100, dev)),
                patch("release.check_separation", return_value={}),
                patch("release.load_artifact", return_value=(head, "artifact-hash")),
                patch("sentence_transformers.SentenceTransformer") as constructor,
                patch("release.features", return_value=matrix) as extract,
            ):
                encoder = constructor.return_value
                encoder.max_seq_length = CONTEXT_TOKENS_MAX
                encoder.__getitem__.return_value.auto_model.config.max_position_embeddings = CONTEXT_TOKENS_MAX
                with self.assertRaisesRegex(ValueError, "scaled exactly once"):
                    main()
                extract.assert_called_once_with(encoder, dev)
                self.assertFalse(output.exists())

    def test_sources_require_synthetic_and_independent_review_metadata(self) -> None:
        source = {
            "provenance": "fully_synthetic_assistant_authored",
            "review_status": "blind_reviewer_agreed_independence_self_attested",
        }
        validate_reviewed_source(source)
        for field in source:
            with self.subTest(field=field), self.assertRaises(ValueError):
                validate_reviewed_source({**source, field: "unreviewed"})


if __name__ == "__main__":
    unittest.main()
