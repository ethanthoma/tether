import copy
import json
import unittest

import numpy as np
from build_synthetic import expand_scenarios
from calibrate import (
    check_separation,
    fit_temperature,
    load_partition,
    negative_log_likelihood,
    scaled_probabilities,
)
from train import LABELS, ROOT, load_cases, predictions, select_thresholds


class CalibrationTests(unittest.TestCase):
    def test_new_partitions_are_disjoint_and_have_visible_evidence(self) -> None:
        prior = expand_scenarios(
            json.loads((ROOT / "synthetic_scenarios.json").read_text())
        )
        prior += load_cases(ROOT / "seed.json") + load_cases(ROOT / "cases.json")
        partitions = [
            load_partition(ROOT / f"calibration-{split}.json", split)
            for split in ("temperature", "selection", "audit")
        ]
        report = check_separation([prior, *partitions])
        self.assertEqual(report["partition_counts"][1:], [25, 25, 25])
        duplicate = copy.deepcopy(partitions)
        duplicate[-1][0]["messages"] = duplicate[0][0]["messages"]
        with self.assertRaisesRegex(ValueError, "message crosses partitions"):
            check_separation(duplicate)

    def test_temperature_preserves_classes_and_reduces_overconfidence_loss(
        self,
    ) -> None:
        logits = np.tile(np.array([[8.0, 0.0, -1.0, -2.0, -3.0]]), (10, 1))
        labels = np.array([0] * 7 + [1] * 3)
        temperature = fit_temperature(logits, labels)
        self.assertGreater(temperature, 1)
        self.assertLess(
            negative_log_likelihood(logits, labels, temperature),
            negative_log_likelihood(logits, labels, 1),
        )
        calibrated = scaled_probabilities(logits, temperature)
        np.testing.assert_array_equal(calibrated.argmax(axis=1), logits.argmax(axis=1))
        np.testing.assert_allclose(calibrated.sum(axis=1), 1)

    def test_nonfinite_values_and_invalid_temperatures_fail(self) -> None:
        for temperature in (0, -1, float("inf"), float("nan")):
            with self.assertRaises(ValueError):
                scaled_probabilities(np.zeros((1, 5)), temperature)
        with self.assertRaises(ValueError):
            scaled_probabilities(np.array([[float("inf")]]), 1)

    def test_abstain_all_survives_floating_point_saturation(self) -> None:
        head = {"labels": LABELS}
        values = np.array([[0.0, 1.0, 0.0, 0.0, 0.0]])
        cases = [{"expected": "needs_reply"}]
        thresholds = select_thresholds(head, cases, values)
        self.assertEqual(thresholds, dict.fromkeys(LABELS, 1.0))
        self.assertEqual(predictions(head, values, thresholds), ["abstain"])


if __name__ == "__main__":
    unittest.main()
