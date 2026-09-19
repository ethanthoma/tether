import copy
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import numpy as np
from train import (
    ROOT,
    features,
    load_cases,
    load_dataset,
    predictions,
    score,
    train_model,
    training_split,
)


class EncoderStub:
    max_seq_length = 256

    def __init__(self) -> None:
        self.tokenizer = self

    def encode(self, text: str | list[str], **kwargs: object) -> list[str] | np.ndarray:
        if isinstance(text, str):
            return text.split()
        return np.ones((len(text), 2), dtype=np.float32)


class TrainingTests(unittest.TestCase):
    def test_dataset_policy_rejects_unknown_and_mixed_contracts(self) -> None:
        source = json.loads((ROOT / "seed.json").read_text())
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "data.json"
            for policy in (None, "synthetic-obligations-v1", "reply-triage-v2"):
                document = copy.deepcopy(source)
                if policy is not None:
                    document["label_policy"] = policy
                path.write_text(json.dumps(document))
                self.assertEqual(
                    load_dataset(path)[1], policy or "synthetic-obligations-v1"
                )
            for policy in ("unknown", None, []):
                document = {**source, "label_policy": policy}
                path.write_text(json.dumps(document))
                with self.assertRaisesRegex(ValueError, "label policy"):
                    load_dataset(path)
            document = copy.deepcopy(source)
            document["cases"][0]["label_policy"] = "reply-triage-v2"
            path.write_text(json.dumps(document))
            with self.assertRaisesRegex(ValueError, "differs"):
                load_dataset(path)

    def test_training_records_dataset_policy_in_artifact_and_report(self) -> None:
        source = json.loads((ROOT / "seed.json").read_text())
        source["label_policy"] = "reply-triage-v2"
        encoder = EncoderStub()
        encoder.save_pretrained = lambda *args, **kwargs: None
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "data.json"
            output = Path(directory) / "model"
            path.write_text(json.dumps(source))
            with (
                patch(
                    "sentence_transformers.SentenceTransformer", return_value=encoder
                ),
                patch("builtins.print"),
            ):
                train_model(path, output)
            for name in ("head.json", "training.json"):
                self.assertEqual(
                    json.loads((output / name).read_text())["label_policy"],
                    "reply-triage-v2",
                )

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
