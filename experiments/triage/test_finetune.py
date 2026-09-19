import unittest

import numpy as np
import torch
from finetune import batch_features
from train import CONTEXT_TOKENS_MAX, LABELS, features, predictions, select_thresholds


class DifferentiableEncoder(torch.nn.Module):
    max_seq_length = CONTEXT_TOKENS_MAX

    def __init__(self) -> None:
        super().__init__()
        self.vector = torch.nn.Parameter(torch.arange(1.0, 385.0))

    @property
    def tokenizer(self) -> "DifferentiableEncoder":
        return self

    def preprocess(self, inputs: list[str]) -> dict:
        return {"count": len(inputs)}

    def forward(self, inputs: dict) -> dict:
        return {"sentence_embedding": self.vector.expand(inputs["count"], -1)}

    def encode(self, text: str | list[str], **kwargs: object) -> list[str] | np.ndarray:
        if isinstance(text, str):
            assert kwargs.get("truncation") is False
            return ["[CLS]", *text.split(), "[SEP]"]
        normalized = torch.nn.functional.normalize(self.vector, dim=0)
        return normalized.detach().expand(len(text), -1).numpy()


class FinetuningTests(unittest.TestCase):
    def test_layout_matches_frozen_inference_and_backpropagates(self) -> None:
        cases = [
            {"messages": [{"outbound": False, "body": "First"}]},
            {"messages": [{"outbound": True, "body": "Second"}]},
            {
                "messages": [
                    {"outbound": False, "body": "Third"},
                    {"outbound": True, "body": "Fourth"},
                ]
            },
            {
                "messages": [
                    {"outbound": True, "body": "Fifth"},
                    {"outbound": False, "body": "Sixth"},
                ]
            },
        ]
        encoder = DifferentiableEncoder()
        matrix = batch_features(encoder, cases)
        np.testing.assert_allclose(matrix.detach().numpy(), features(encoder, cases))
        matrix.sum().backward()
        self.assertIsNotNone(encoder.vector.grad)
        self.assertGreater(float(encoder.vector.grad.abs().sum()), 0)
        self.assertEqual(matrix.shape, (4, 388))
        np.testing.assert_array_equal(
            matrix.detach().numpy()[:, 384:],
            [[0, 0, 1, 0], [0, 0, 0, 1], [1, 0, 0, 1], [0, 1, 1, 0]],
        )

    def test_batch_and_token_limits(self) -> None:
        encoder = DifferentiableEncoder()
        with self.assertRaisesRegex(ValueError, "cases per batch"):
            batch_features(encoder, [])
        with self.assertRaisesRegex(ValueError, "cases per batch"):
            batch_features(encoder, [{}] * 17)
        with self.assertRaisesRegex(ValueError, "token limit"):
            batch_features(
                encoder, [{"messages": [{"outbound": False, "body": "word " * 513}]}]
            )

    def test_combined_context_limit_rejects_differentiable_batch(self) -> None:
        encoder = DifferentiableEncoder()
        cases = [
            {
                "messages": [
                    {"outbound": False, "body": "word " * 260},
                    {"outbound": True, "body": "word " * 260},
                ]
            }
        ]
        with self.assertRaisesRegex(ValueError, "joint thread exceeds"):
            batch_features(encoder, cases)

    def test_cutoff_rejects_errors_and_maximizes_correct_coverage(self) -> None:
        head = {"labels": LABELS}
        cases = [{"expected": "needs_reply"}, {"expected": "needs_reply"}]
        values = np.array([[0.0, 0.1, 0.9, 0.0, 0.0], [0.0, 0.8, 0.2, 0.0, 0.0]])
        expected = {**dict.fromkeys(LABELS, 1.0), "needs_reply": 0.0}
        thresholds = select_thresholds(head, cases, values)
        self.assertEqual(thresholds, expected)
        self.assertEqual(
            predictions(head, values, thresholds), ["needs_reply", "abstain"]
        )
        correct_values = np.array(
            [[0.0, 0.1, 0.9, 0.0, 0.0], [0.0, 0.2, 0.8, 0.0, 0.0]]
        )
        self.assertEqual(select_thresholds(head, cases, correct_values), expected)


if __name__ == "__main__":
    unittest.main()
