import copy
import json
import unittest

import numpy as np
from build_synthetic import audit_cases, expand_scenarios, obligation_label
from mdl import prequential
from train import LABELS, ROOT, load_cases


class SyntheticTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.source = json.loads((ROOT / "synthetic_scenarios.json").read_text())
        cls.cases = expand_scenarios(cls.source)

    def test_obligation_direction_and_invariants(self) -> None:
        self.assertEqual(obligation_label("recipient", False), "needs_reply")
        self.assertEqual(obligation_label("sender", True), "needs_reply")
        self.assertEqual(obligation_label("recipient", True), "waiting_on_them")
        self.assertEqual(obligation_label("sender", False), "waiting_on_them")
        for state in ("both", "unknown", "none", "bulk"):
            self.assertEqual(
                obligation_label(state, True), obligation_label(state, False)
            )
        with self.assertRaisesRegex(ValueError, "unknown obligation"):
            obligation_label("guess", False)

    def test_missing_evidence_is_rejected(self) -> None:
        broken = copy.deepcopy(self.source)
        broken["scenarios"][0]["variants"][0]["evidence"] = "not present in the thread"
        with self.assertRaisesRegex(ValueError, "evidence must quote"):
            expand_scenarios(broken)

    def test_every_split_covers_all_behaviors_without_family_leakage(self) -> None:
        audit = audit_cases(self.cases)
        self.assertEqual(audit["families"], 72)
        expected_capabilities = set(audit["splits"]["train"]["capabilities"])
        self.assertEqual(len(expected_capabilities), 12)
        for split in audit["splits"].values():
            self.assertEqual(set(split["labels"]), set(LABELS))
            self.assertEqual(set(split["capabilities"]), expected_capabilities)
        bodies = {m["body"] for case in self.cases for m in case["messages"]}
        for filename in ("seed.json", "cases.json"):
            previous = {
                m["body"]
                for case in load_cases(ROOT / filename)
                for m in case["messages"]
            }
            self.assertFalse(bodies & previous)

    def test_exact_and_near_duplicates_cannot_cross_to_test(self) -> None:
        crossed = copy.deepcopy(self.cases)
        crossed[-1]["messages"] = copy.deepcopy(crossed[0]["messages"])
        with self.assertRaisesRegex(ValueError, "message crosses splits"):
            audit_cases(crossed)
        crossed[-1]["messages"][0]["body"] += " Thank you kindly."
        with self.assertRaisesRegex(ValueError, "near-duplicate crosses splits"):
            audit_cases(crossed)

    def test_prequential_code_rewards_predictable_labels_over_shuffled_labels(
        self,
    ) -> None:
        labels = np.tile(np.arange(5), 40)
        matrix = np.eye(5)[labels]
        groups = [f"family_{i // 5:02}" for i in range(len(labels))]
        learned = prequential(matrix, labels, groups, 42)
        shuffled = prequential(
            matrix, np.random.default_rng(0).permutation(labels), groups, 42
        )
        self.assertLess(learned["learner_bits"], learned["prior_bits"])
        self.assertLess(learned["learner_bits"], shuffled["learner_bits"])
        self.assertEqual(
            sum(block["encoded_cases"] for block in learned["blocks"]), len(labels)
        )
        self.assertEqual(learned["blocks"][0]["train_families"], 0)
        self.assertEqual(learned, prequential(matrix, labels, groups, 42))


if __name__ == "__main__":
    unittest.main()
