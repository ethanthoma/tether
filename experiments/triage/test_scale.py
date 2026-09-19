import json
import unittest

from build_synthetic import audit_cases, expand_scenarios
from calibrate import check_separation
from scale import nested_datasets
from train import ROOT, load_cases, training_split


class ScalingTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.original = expand_scenarios(
            json.loads((ROOT / "synthetic_scenarios.json").read_text())
        )
        cls.added = expand_scenarios(
            json.loads((ROOT / "scale_scenarios.json").read_text())
        )

    def test_nested_sets_keep_entire_families_and_identical_development(self) -> None:
        datasets = nested_datasets(self.original, self.added)
        self.assertEqual(datasets, nested_datasets(self.original, self.added))
        previous_ids, expected_dev = set(), None
        for name, family_count in (("small", 48), ("medium", 98), ("full", 148)):
            train, dev = training_split(datasets[name])
            ids = {case["id"] for case in train}
            groups = {case["group"] for case in train}
            self.assertLess(previous_ids, ids)
            self.assertEqual(len(groups), family_count)
            self.assertEqual(len(dev), 207)
            if expected_dev is not None:
                self.assertEqual(dev, expected_dev)
            for case in self.original + self.added:
                if case["group"] in groups:
                    self.assertEqual(case["split"], "train")
                    self.assertIn(case["id"], ids)
            previous_ids, expected_dev = ids, dev
        self.assertEqual(len(previous_ids), 981)

    def test_new_cases_exclude_prior_evaluations_and_holdout_families(self) -> None:
        previous = list(self.original)
        for filename in (
            "seed.json",
            "cases.json",
            "calibration-temperature.json",
            "calibration-selection.json",
            "calibration-audit.json",
        ):
            previous.extend(load_cases(ROOT / filename))
        check_separation([previous, self.added])
        combined = [
            case for case in self.original if case["split"] != "test"
        ] + self.added
        audit = audit_cases(combined)
        self.assertEqual(audit["splits"]["test"]["cases"], 144)
        self.assertEqual(audit["splits"]["test"]["families"], 20)
        self.assertEqual(audit["splits"]["train"]["labels"]["noise"], 46)


if __name__ == "__main__":
    unittest.main()
