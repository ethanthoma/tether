import io
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import numpy as np
from shadow import (
    REQUEST_BYTES_MAX,
    classify,
    load_artifact,
    read_request,
    validate_head,
)
from train import LABELS


class EncoderStub:
    max_seq_length = 256

    def __init__(self) -> None:
        self.tokenizer = self
        self.calls = 0

    def encode(
        self, texts: str | list[str], **kwargs: object
    ) -> list[str] | np.ndarray:
        if isinstance(texts, str):
            return texts.split()
        self.calls += 1
        return np.zeros((len(texts), 384), dtype=np.float32)


def head() -> dict:
    return {
        "version": 1,
        "labels": LABELS,
        "coefficients": np.zeros((5, 1540)).tolist(),
        "intercepts": [0, 0, 5, 0, 0],
        "threshold": 0.8,
    }


class ShadowTests(unittest.TestCase):
    def test_batch_rejects_individual_inputs_and_preserves_order(self) -> None:
        cases = [
            {"id": "good", "messages": [{"outbound": False, "body": "Please reply"}]},
            {"id": "missing"},
            {"id": "long", "messages": [{"outbound": False, "body": "word " * 257}]},
            {"id": "bytes", "messages": [{"outbound": False, "body": "é" * 2001}]},
            {"id": "direction", "messages": [{"outbound": 1, "body": "Hello"}]},
            {"id": "empty", "messages": []},
            {"id": "unicode", "messages": [{"outbound": False, "body": "\ud800"}]},
        ]
        encoder = EncoderStub()
        result = classify(cases, head(), encoder, "a" * 64)
        self.assertEqual(encoder.calls, 1)
        self.assertEqual(result["policy"], "synthetic-obligations-v1")
        self.assertEqual(
            [row["id"] for row in result["results"]], [case["id"] for case in cases]
        )
        self.assertEqual(result["results"][0]["label"], "needs_reply")
        self.assertGreater(result["results"][0]["confidence"], 0.9)
        for row in result["results"][1:]:
            self.assertEqual(
                (row["label"], row["confidence"], row["status"]),
                ("abstain", 0, "input_rejected"),
            )
        self.assertNotIn("Please reply", json.dumps(result))
        abstaining = head()
        abstaining["threshold"] = 1
        row = classify(cases[:1], abstaining, encoder, "a" * 64)["results"][0]
        self.assertEqual(row["label"], "abstain")
        self.assertGreater(row["confidence"], 0.9)
        v2 = {**head(), "label_policy": "reply-triage-v2"}
        self.assertEqual(
            classify(cases[:1], v2, encoder, "a" * 64)["policy"], "reply-triage-v2"
        )

    def test_request_bounds_and_identifiers(self) -> None:
        valid = {"version": 1, "cases": [{"id": "opaque"}]}
        self.assertEqual(
            read_request(io.BytesIO(json.dumps(valid).encode())), valid["cases"]
        )
        invalid = [
            [],
            {"version": True, "cases": valid["cases"]},
            {"version": 1, "cases": []},
        ]
        invalid += [{"version": 1, "cases": [{"id": "same"}] * 2}]
        invalid += [{"version": 1, "cases": [{"id": str(i)} for i in range(21)]}]
        invalid += [{"version": 1, "cases": [{"id": "x" * 129}]}]
        for request in invalid:
            with self.subTest(request=request), self.assertRaises(ValueError):
                read_request(io.BytesIO(json.dumps(request).encode()))
        with self.assertRaises(ValueError):
            read_request(io.BytesIO(b" " * (REQUEST_BYTES_MAX + 1)))

    def test_head_validation_and_nonfinite_inference(self) -> None:
        for field, value in [
            ("threshold", float("nan")),
            ("threshold", True),
            ("labels", LABELS[:4]),
            ("coefficients", [[0]]),
            ("label_policy", "unknown"),
            ("label_policy", None),
        ]:
            invalid = head()
            invalid[field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                validate_head(invalid)
        invalid = head()
        invalid["intercepts"][0] = float("inf")
        with self.assertRaises(ValueError):
            validate_head(invalid)
        encoder = EncoderStub()
        encoder.encode = lambda texts, **kwargs: (
            texts.split()
            if isinstance(texts, str)
            else np.full((len(texts), 384), np.nan)
        )
        with self.assertRaises(ValueError):
            classify(
                [{"id": "a", "messages": [{"outbound": False, "body": "Hi"}]}],
                head(),
                encoder,
                "a" * 64,
            )

    def test_identity_covers_tokenizer_weights_and_head(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            model = Path(directory)
            (model / "encoder").mkdir()
            (model / "head.json").write_text(json.dumps(head()))
            weights = model / "encoder" / "model.safetensors"
            weights.write_bytes(b"test weights")
            tokenizer = model / "encoder" / "tokenizer.json"
            tokenizer.write_text("{}")
            _, original = load_artifact(model)
            self.assertEqual(load_artifact(model)[1], original)
            (model / "head.json").write_text(
                json.dumps({**head(), "label_policy": "reply-triage-v2"})
            )
            loaded, policy_changed = load_artifact(model)
            self.assertEqual(loaded["label_policy"], "reply-triage-v2")
            self.assertNotEqual(policy_changed, original)
            tokenizer.write_text('{"changed":true}')
            changed = load_artifact(model)[1]
            self.assertNotEqual(changed, policy_changed)
            weights.write_bytes(b"different weights")
            self.assertNotEqual(load_artifact(model)[1], changed)
            weights.unlink()
            weights.symlink_to(model / "head.json")
            with self.assertRaises(ValueError):
                load_artifact(model)

    def test_cli_errors_never_echo_input(self) -> None:
        result = subprocess.run(
            [
                sys.executable,
                str(Path(__file__).with_name("shadow.py")),
                "--model",
                "/missing",
            ],
            input=b'{"private message":',
            capture_output=True,
            timeout=20,
            check=False,
        )
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, b"")
        self.assertEqual(result.stderr, b"shadow inference failed\n")


if __name__ == "__main__":
    unittest.main()
