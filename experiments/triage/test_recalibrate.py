import copy
import hashlib
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import numpy as np
from recalibrate import (
    cutoff_thresholds,
    evaluate,
    fit,
    prepare,
    readiness,
    validate_candidate,
)
from shadow import load_artifact
from train import FEATURE_LAYOUT, HEAD_VERSION, LABELS


class RecalibrationTests(unittest.TestCase):
    def test_frozen_model_cannot_admit_changed_training_provenance(self) -> None:
        head = {
            "version": HEAD_VERSION,
            "feature_layout": FEATURE_LAYOUT,
            "label_policy": "reply-triage-v2",
            "labels": LABELS,
            "coefficients": np.zeros((5, 388)).tolist(),
            "intercepts": [0.0] * 5,
            "thresholds": dict.fromkeys(LABELS, 1.0),
        }
        original_data = b'{"cases":[]}'
        data_hash = hashlib.sha256(original_data).hexdigest()
        original_report = json.dumps({"data_sha256": data_hash}).encode()
        report_hash = hashlib.sha256(original_report).hexdigest()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "encoder").mkdir()
            (root / "encoder/model.safetensors").write_bytes(b"frozen weights")
            (root / "head.json").write_text(json.dumps(head))
            identity = load_artifact(root)[1]
            for change_data, change_report in (
                (True, False),
                (False, True),
                (True, True),
            ):
                data = original_data + b" " if change_data else original_data
                report = (
                    json.dumps(
                        {
                            "data_sha256": hashlib.sha256(data).hexdigest(),
                            "changed": True,
                        }
                    ).encode()
                    if change_report
                    else original_report
                )
                (root / "data.json").write_bytes(data)
                (root / "training.json").write_bytes(report)
                self.assertEqual(load_artifact(root)[1], identity)
                with (
                    self.subTest(change_data=change_data, change_report=change_report),
                    patch("recalibrate.SOURCE_SHA256", identity),
                    patch("recalibrate.TRAINING_DATA_SHA256", data_hash),
                    patch("recalibrate.TRAINING_REPORT_SHA256", report_hash),
                    patch("recalibrate.reviewed_cases") as datasets,
                    patch("recalibrate.features") as inference,
                    patch("sentence_transformers.SentenceTransformer") as encoder,
                ):
                    with self.assertRaisesRegex(ValueError, "frozen identity"):
                        prepare(
                            root,
                            root / "data.json",
                            root / "calibration.json",
                            root / "plan.md",
                        )
                    datasets.assert_not_called()
                    inference.assert_not_called()
                    encoder.assert_not_called()

    def test_reserved_report_prohibits_heldout_features(self) -> None:
        cases = [
            {
                "id": str(index),
                "split": "test",
                "group": f"family-{index // 2}",
                "expected": LABELS[index % 5],
            }
            for index in range(80)
        ]
        context = {"train": [], "dev": [], "calibration": [], "encoder": object()}
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            output = root / "reserved.json"
            output.write_text("")
            with (
                patch("recalibrate.validate_candidate", return_value=({}, "a" * 64)),
                patch(
                    "recalibrate.calibration_report", return_value={"approved": True}
                ),
                patch(
                    "recalibrate.read_unscaled_head", return_value=b'{"approved":true}'
                ),
                patch("recalibrate.reviewed_cases", return_value=cases),
                patch("recalibrate.check_separation", return_value={}),
                patch("recalibrate.features") as inference,
            ):
                with self.assertRaises(FileExistsError):
                    evaluate(context, root, root / "test.json", output)
                inference.assert_not_called()

    def test_cutoffs_only_tighten_with_actionable_floor(self) -> None:
        head = {
            "labels": LABELS,
            "thresholds": {
                "abstain": 1.0,
                "fyi": 0.9,
                "needs_reply": 0.8,
                "noise": 0.8,
                "waiting_on_them": 0.975,
            },
        }
        cases = [
            {"expected": label}
            for label in ["fyi", "needs_reply", "noise", "waiting_on_them", "abstain"]
        ]
        values = np.array(
            [
                [0.005, 0.98, 0.005, 0.005, 0.005],
                [0.01, 0.01, 0.96, 0.01, 0.01],
                [0.025, 0.025, 0.025, 0.9, 0.025],
                [0.0025, 0.0025, 0.0025, 0.0025, 0.99],
                [0.015, 0.015, 0.94, 0.015, 0.015],
            ]
        )
        before = copy.deepcopy(head)
        self.assertEqual(
            cutoff_thresholds(head, cases, values),
            {
                "abstain": 1.0,
                "fyi": 0.9,
                "needs_reply": 0.95,
                "noise": 0.8,
                "waiting_on_them": 0.975,
            },
        )
        self.assertEqual(head, before)
        low = {
            "labels": LABELS,
            "thresholds": {
                label: 1.0 if label == "abstain" else 0.0 for label in LABELS
            },
        }
        selected = cutoff_thresholds(low, cases[:4], values[:4])
        self.assertEqual(selected["needs_reply"], 0.9)
        self.assertEqual(selected["waiting_on_them"], 0.9)
        absent = cutoff_thresholds(
            low, [{"expected": "noise"}], np.array([[0, 0, 0, 1, 0]])
        )
        self.assertEqual(absent["needs_reply"], 1)
        self.assertEqual(absent["waiting_on_them"], 1)
        saturated = cutoff_thresholds(
            low, [{"expected": "abstain"}], np.array([[0, 0, 1, 0, 0]])
        )
        self.assertEqual(saturated["needs_reply"], 1)

    def test_readiness_keeps_failed_evidence_without_relaxing_gate(self) -> None:
        cases = [{"split": "dev", "expected": label} for label in LABELS]
        report = readiness(cases, LABELS)
        self.assertTrue(report["approved"])
        failed = readiness(cases, ["abstain"] * 5)
        self.assertFalse(failed["approved"])
        self.assertEqual(failed["coverage"], 0)
        self.assertEqual(
            failed["actionable_correct"], {"needs_reply": 0, "waiting_on_them": 0}
        )

    def test_candidate_preserves_source_and_rejects_tampering_before_holdout(
        self,
    ) -> None:
        head = {
            "version": HEAD_VERSION,
            "feature_layout": FEATURE_LAYOUT,
            "label_policy": "reply-triage-v2",
            "labels": LABELS,
            "coefficients": np.zeros((5, 388)).tolist(),
            "intercepts": [0.0] * 5,
            "thresholds": {
                label: 1.0 if label == "abstain" else 0.8 for label in LABELS
            },
            "temperature": 2.0,
        }
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "source"
            (source / "encoder").mkdir(parents=True)
            (source / "encoder/model.safetensors").write_bytes(b"original weights")
            (source / "head.json").write_text(json.dumps(head))
            training, unscaled = b'{"source":"training"}', b'{"source":"unscaled"}'
            (source / "training.json").write_bytes(training)
            (source / "unscaled-head.json").write_bytes(unscaled)
            _, identity = load_artifact(source)
            candidate = {
                **head,
                "thresholds": dict.fromkeys(LABELS, 1.0),
                "cutoff_calibration": {"source_model_sha256": identity},
            }
            context = {
                "source_model": source,
                "candidate": candidate,
                "training_bytes": training,
                "unscaled_bytes": unscaled,
                "provenance": {"source_model_sha256": identity},
                "separation": {},
                "original_development_readiness": {"approved": False},
                "calibration_readiness": {"approved": False},
            }
            with patch("recalibrate.SOURCE_SHA256", identity), patch("builtins.print"):
                output = root / "candidate"
                report = fit(context, output)
                self.assertFalse(report["approved"])
                self.assertTrue((output / "calibration.json").is_file())
                self.assertEqual(
                    (source / "encoder/model.safetensors").read_bytes(),
                    b"original weights",
                )
                validate_candidate(context, output)
                with (
                    patch("recalibrate.reviewed_cases") as heldout,
                    patch("recalibrate.features") as inference,
                ):
                    with self.assertRaisesRegex(ValueError, "readiness"):
                        evaluate(
                            context,
                            output,
                            root / "unread-test.json",
                            root / "report.json",
                        )
                    heldout.assert_not_called()
                    inference.assert_not_called()
                with self.assertRaises(FileExistsError):
                    fit(context, output)
                mutated = {**candidate, "temperature": 3.0}
                (output / "head.json").write_text(json.dumps(mutated))
                with self.assertRaisesRegex(ValueError, "more than"):
                    validate_candidate(context, output)
                (output / "head.json").write_text(json.dumps(candidate))
                (output / "encoder/model.safetensors").write_bytes(b"different weights")
                with self.assertRaisesRegex(ValueError, "encoder"):
                    validate_candidate(context, output)
                (output / "encoder/model.safetensors").write_bytes(b"original weights")
                (output / "unscaled-head.json").write_bytes(b"{}")
                with self.assertRaisesRegex(ValueError, "training metadata"):
                    validate_candidate(context, output)


if __name__ == "__main__":
    unittest.main()
