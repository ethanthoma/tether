import copy
import unittest

import numpy as np
from train import ROOT, features, load_cases, predictions, score, training_split


class EncoderStub:
    max_seq_length = 256

    def __init__(self) -> None:
        self.tokenizer = self

    def encode(self, text: str | list[str], **kwargs: object) -> list[str] | np.ndarray:
        if isinstance(text, str):
            return text.split()
        return np.ones((len(text), 2), dtype=np.float32)


class TrainingTests(unittest.TestCase):
    def test_direction_and_order_are_preserved_without_labels(self) -> None:
        inbound = {
            "id": "a",
            "expected": "needs_reply",
            "messages": [{"outbound": False, "body": "Hello"}],
        }
        outbound = copy.deepcopy(inbound)
        outbound["messages"][0]["outbound"] = True
        changed_label = {
            **inbound,
            "expected": "noise",
            "rationale": "ignore this",
            "id": "b",
        }
        vectors = features(EncoderStub(), [inbound, outbound, changed_label])
        np.testing.assert_array_equal(vectors[0], vectors[2])
        self.assertFalse(np.array_equal(vectors[0], vectors[1]))
        paired = {"messages": inbound["messages"] + outbound["messages"]}
        reversed_pair = {"messages": list(reversed(paired["messages"]))}
        vectors = features(EncoderStub(), [paired, reversed_pair])
        self.assertFalse(np.array_equal(vectors[0], vectors[1]))

    def test_seed_splits_and_challenge_exclusion(self) -> None:
        cases = load_cases(ROOT / "seed.json")
        train, dev = training_split(cases)
        self.assertTrue(train)
        self.assertTrue(dev)
        seed_bodies = {m["body"] for c in cases for m in c["messages"]}
        challenge_bodies = {
            m["body"] for c in load_cases(ROOT / "cases.json") for m in c["messages"]
        }
        self.assertFalse(seed_bodies & challenge_bodies)
        crossed = copy.deepcopy(cases)
        crossed[1]["split"] = "dev"
        with self.assertRaisesRegex(ValueError, "crosses splits"):
            training_split(crossed)
        duplicate = copy.deepcopy(cases)
        duplicate[-1]["messages"] = duplicate[0]["messages"]
        with self.assertRaisesRegex(ValueError, "text crosses splits"):
            training_split(duplicate)

    def test_long_messages_fail_instead_of_silently_truncating(self) -> None:
        with self.assertRaisesRegex(ValueError, "token limit"):
            features(
                EncoderStub(),
                [{"messages": [{"outbound": False, "body": "word " * 257}]}],
            )

    def test_abstention_and_costs(self) -> None:
        head = {"labels": ["fyi", "needs_reply"]}
        self.assertEqual(
            predictions(head, np.array([[0.51, 0.49], [0.05, 0.95]]), 0.8),
            ["abstain", "needs_reply"],
        )
        cases = [
            {"expected": label}
            for label in [
                "needs_reply",
                "waiting_on_them",
                "abstain",
                "needs_reply",
                "fyi",
            ]
        ]
        metrics = score(cases, ["fyi", "needs_reply", "needs_reply", "abstain", "fyi"])
        self.assertEqual(metrics["accepted"], 4)
        self.assertEqual(metrics["correct_accepted"], 1)
        self.assertEqual(metrics["false_reminders"], 2)
        self.assertEqual(metrics["false_silencing"], 1)
        self.assertEqual(metrics["ambiguous_accepted"], 1)
        self.assertEqual(metrics["actionable_abstained"], 1)


if __name__ == "__main__":
    unittest.main()
