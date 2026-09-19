import copy
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

import numpy as np
from train import (
    CONTEXT_TOKENS_MAX,
    FEATURE_LAYOUT,
    HEAD_VERSION,
    LABELS,
    ROOT,
    features,
    load_cases,
    load_dataset,
    predictions,
    score,
    select_thresholds,
    thread_inputs,
    train_model,
    training_split,
    validate_thresholds,
)


class EncoderStub:
    max_seq_length = CONTEXT_TOKENS_MAX

    def __init__(self) -> None:
        self.tokenizer = self
        self.texts = []
        self.position_capacity = 512

    def __getitem__(self, index: int) -> object:
        assert index == 0
        return SimpleNamespace(
            auto_model=SimpleNamespace(
                config=SimpleNamespace(max_position_embeddings=self.position_capacity)
            )
        )

    def encode(self, text: str | list[str], **kwargs: object) -> list[str] | np.ndarray:
        if isinstance(text, str):
            assert kwargs.get("truncation") is False
            return ["[CLS]", *text.split(), "[SEP]"]
        self.texts = text
        return np.ones((len(text), 384), dtype=np.float32)


class TrainingTests(unittest.TestCase):
    def test_base_context_is_extended_only_after_capacity_check(self) -> None:
        cases = [
            {
                "id": f"{split}-{label}",
                "group": f"{split}-{label}",
                "split": split,
                "expected": label,
                "messages": [{"outbound": False, "body": f"{split} {label}"}],
            }
            for split in ("train", "dev")
            for label in LABELS
        ]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "data.json"
            path.write_text(
                json.dumps({"label_policy": "reply-triage-v2", "cases": cases})
            )
            for capacity in (256, 512):
                encoder = EncoderStub()
                encoder.max_seq_length = 256
                encoder.position_capacity = capacity
                encoder.save_pretrained = lambda *args, **kwargs: None
                with (
                    patch(
                        "sentence_transformers.SentenceTransformer",
                        return_value=encoder,
                    ),
                    patch("builtins.print"),
                ):
                    output = Path(directory) / str(capacity)
                    if capacity == 256:
                        with self.assertRaisesRegex(ValueError, "positional capacity"):
                            train_model(path, output)
                        self.assertEqual(encoder.max_seq_length, 256)
                        self.assertEqual(encoder.texts, [])
                    else:
                        train_model(path, output)
                        self.assertEqual(encoder.max_seq_length, 512)
                        artifact = json.loads((output / "head.json").read_text())
                        self.assertEqual(artifact["feature_layout"], FEATURE_LAYOUT)
                        self.assertEqual(
                            np.asarray(artifact["coefficients"]).shape, (5, 388)
                        )

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
            artifact = json.loads((output / "head.json").read_text())
            self.assertEqual(artifact["version"], HEAD_VERSION)
            self.assertEqual(artifact["feature_layout"], FEATURE_LAYOUT)
            self.assertEqual(np.asarray(artifact["coefficients"]).shape, (5, 388))
            report = json.loads((output / "training.json").read_text())
            self.assertEqual(report["max_seq_length"], 512)
            self.assertEqual(report["base_position_capacity"], 512)
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
                [{"messages": [{"outbound": False, "body": "word " * 513}]}],
            )

    def test_joint_serialization_preserves_order_and_direction_bits(self) -> None:
        cases = [
            {
                "messages": [
                    {"outbound": False, "body": "First"},
                    {"outbound": True, "body": "Second"},
                ]
            },
            {
                "messages": [
                    {"outbound": True, "body": "Second"},
                    {"outbound": False, "body": "First"},
                ]
            },
            {
                "messages": [
                    {"outbound": False, "body": "First"},
                    {"outbound": False, "body": "Second"},
                ]
            },
            {
                "messages": [
                    {"outbound": False, "body": "Second"},
                    {"outbound": False, "body": "First"},
                ]
            },
            {"messages": [{"outbound": True, "body": "Only"}]},
        ]
        encoder = EncoderStub()
        texts, present = thread_inputs(encoder, cases)
        self.assertEqual(
            texts[0],
            "Message from the other person:\nFirst\n\n---\n\nMessage from you:\nSecond",
        )
        self.assertEqual(
            texts[1],
            "Message from you:\nSecond\n\n---\n\nMessage from the other person:\nFirst",
        )
        self.assertNotEqual(texts[2], texts[3])
        self.assertEqual(texts[4], "Message from you:\nOnly")
        np.testing.assert_array_equal(
            present,
            [[1, 0, 0, 1], [0, 1, 1, 0], [1, 0, 1, 0], [1, 0, 1, 0], [0, 0, 0, 1]],
        )
        matrix = features(encoder, cases)
        self.assertEqual(encoder.texts, texts)
        self.assertEqual(matrix.shape, (5, 388))
        np.testing.assert_array_equal(matrix[:, 384:], present)

    def test_joint_limit_counts_markers_and_rejects_before_embedding(self) -> None:
        encoder = EncoderStub()
        boundary = {"messages": [{"outbound": False, "body": "word " * 505}]}
        self.assertEqual(features(encoder, [boundary]).shape, (1, 388))
        invalid = [
            {"messages": [{"outbound": False, "body": "word " * 506}]},
            {
                "messages": [
                    {"outbound": False, "body": "word " * 260},
                    {"outbound": True, "body": "word " * 260},
                ]
            },
        ]
        for case in invalid:
            encoder.texts = []
            for message in case["messages"]:
                self.assertLess(len(message["body"].split()) + 2, CONTEXT_TOKENS_MAX)
            with self.assertRaisesRegex(ValueError, "joint thread exceeds"):
                features(encoder, [case])
            self.assertEqual(encoder.texts, [])
        encoder.max_seq_length = 256
        with self.assertRaisesRegex(ValueError, "512-token"):
            features(encoder, [boundary])

    def test_abstention_and_costs(self) -> None:
        head = {"labels": ["fyi", "needs_reply"]}
        self.assertEqual(
            predictions(
                head,
                np.array([[0.51, 0.49], [0.05, 0.95]]),
                {label: 1.0 if label == "abstain" else 0.8 for label in LABELS},
            ),
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

    def test_classwise_thresholds_keep_safe_classes_and_disable_unsupported(
        self,
    ) -> None:
        head = {"labels": LABELS}
        cases = [
            {"expected": label}
            for label in ["fyi", "abstain", "needs_reply", "waiting_on_them", "noise"]
        ]
        values = np.array(
            [
                [0.025, 0.9, 0.025, 0.025, 0.025],
                [0.01, 0.01, 0.974, 0.003, 0.003],
                [0.005, 0.005, 0.98, 0.005, 0.005],
                [0.005, 0.005, 0.005, 0.005, 0.98],
                [0, 0, 0, 0, 1],
            ]
        )
        thresholds = select_thresholds(head, cases, values)
        self.assertEqual(
            thresholds,
            {
                "abstain": 1,
                "fyi": 0,
                "needs_reply": 0.975,
                "noise": 1,
                "waiting_on_them": 1,
            },
        )
        self.assertEqual(
            predictions(head, values, thresholds),
            ["fyi", "abstain", "needs_reply", "abstain", "abstain"],
        )
        self.assertEqual(predictions(head, values)[-1], "waiting_on_them")

    def test_threshold_schema_rejects_missing_unknown_and_nonfinite_values(
        self,
    ) -> None:
        valid = dict.fromkeys(LABELS, 1.0)
        invalid = [
            None,
            0.8,
            {},
            {**valid, "extra": 0.5},
            {key: value for key, value in valid.items() if key != "noise"},
        ]
        invalid += [
            {**valid, "fyi": value}
            for value in [None, True, float("nan"), float("inf"), -0.1, 1.1]
        ]
        invalid.append({**valid, "abstain": 0.9})
        for thresholds in invalid:
            with self.subTest(thresholds=thresholds), self.assertRaises(ValueError):
                validate_thresholds(thresholds)


if __name__ == "__main__":
    unittest.main()
