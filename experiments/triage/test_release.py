import copy
import unittest

from release import (
    release_metrics,
    validate_reviewed_source,
    validate_test,
    validate_training,
)
from train import BASE, LABELS, REVISION


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

    def test_training_provenance_rejects_recipe_input_and_selection_changes(
        self,
    ) -> None:
        head = {
            "base": BASE,
            "revision": REVISION,
            "threshold": 0.8,
            "encoder_frozen": False,
        }
        report = {
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
            "checkpoint_selection": "lowest unweighted development cross entropy, including epoch zero",
            "threshold_selection": "maximum development coverage with zero accepted errors on the original fixed grid",
            "threshold": 0.8,
            "selected_epoch": 12,
            "encoder_frozen": False,
            "history": [{"epoch": i, "dev_loss": 13.0 - i} for i in range(13)],
        }
        validate_training(report, head, "input-hash", 100, 20)
        for field, value in (
            ("seed", 43),
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
            ("threshold", 0.9),
            ("selected_epoch", 11),
            ("selected_epoch", True),
            ("encoder_frozen", True),
            ("history", report["history"][1:]),
        ):
            with self.subTest(field=field), self.assertRaises(ValueError):
                validate_training({**report, field: value}, head, "input-hash", 100, 20)
        for loss in (float("nan"), float("inf"), -1):
            changed = copy.deepcopy(report)
            changed["history"][0]["dev_loss"] = loss
            with self.subTest(loss=loss), self.assertRaises(ValueError):
                validate_training(changed, head, "input-hash", 100, 20)
        tied = copy.deepcopy(report)
        tied["history"][0]["dev_loss"] = 1.0
        with self.assertRaises(ValueError):
            validate_training(tied, head, "input-hash", 100, 20)
        tied["selected_epoch"] = 0
        tied["encoder_frozen"] = True
        validate_training(tied, {**head, "encoder_frozen": True}, "input-hash", 100, 20)

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
