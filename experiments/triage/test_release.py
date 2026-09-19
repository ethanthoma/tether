import unittest

from release import release_metrics, validate_test
from train import LABELS


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


if __name__ == "__main__":
    unittest.main()
