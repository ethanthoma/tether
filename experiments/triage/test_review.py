import contextlib
import copy
import hashlib
import io
import json
import tempfile
import unittest
from pathlib import Path

from review import prepare_review, reconcile_review, validate_reviews


class ReviewTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.data = self.root / "data.json"
        cases = []
        for i, (split, group) in enumerate(
            (
                ("train", "first"),
                ("train", "first"),
                ("train", "second"),
                ("train", "second"),
                ("dev", "dev"),
                ("test", "test"),
            )
        ):
            cases.append(
                {
                    "id": f"author_fyi_{i}",
                    "group": group,
                    "split": split,
                    "expected": "fyi",
                    "evidence": "AUTHOR EVIDENCE",
                    "obligation_state": "none",
                    "capability": "resolution",
                    "messages": [
                        {
                            "outbound": bool(i % 2),
                            "body": f"Delivery {i} arrived; no further action is needed.",
                            "private_annotation": "fyi",
                        }
                    ],
                }
            )
        self.data.write_text(
            json.dumps(
                {
                    "version": 1,
                    "provenance": "fully_synthetic_assistant_authored",
                    "cases": cases,
                }
            )
        )
        self.bundle = self.root / "bundle"
        with contextlib.redirect_stdout(io.StringIO()):
            prepare_review(self.data, "author", self.bundle)
        self.packet_path = self.bundle / "reviewer/packet.json"
        self.packet = json.loads(self.packet_path.read_text())
        self.digest = hashlib.sha256(self.packet_path.read_bytes()).hexdigest()
        self.manifest = json.loads((self.bundle / "author/manifest.json").read_text())
        self.response = {
            "version": 1,
            "packet_sha256": self.digest,
            "reviewer": "test-reviewer",
            "author_labels_unseen": True,
            "reviews": [
                {
                    "id": case["id"],
                    "label": "fyi",
                    "evidence": [
                        {"message_index": 0, "quote": case["messages"][0]["body"]}
                    ],
                    "rationale": "The delivery is complete with no remaining action.",
                    "flags": [],
                }
                for case in self.packet["cases"]
            ],
        }

    def reconcile(self, response: dict) -> tuple[dict, Path]:
        responses = self.root / "response.json"
        responses.write_text(json.dumps(response))
        output = self.root / "reconciled"
        with contextlib.redirect_stdout(io.StringIO()):
            reconcile_review(self.bundle, responses, output)
        return json.loads((output / "report.json").read_text()), output

    def test_packet_hides_labels_metadata_and_evaluation_cases(self) -> None:
        self.assertEqual(set(self.packet), {"version", "specification", "cases"})
        self.assertEqual(len(self.packet["cases"]), 4)
        for case in self.packet["cases"]:
            self.assertEqual(set(case), {"id", "messages"})
            self.assertNotIn("author_fyi", case["id"])
            self.assertEqual(set(case["messages"][0]), {"outbound", "body"})
        template = json.loads((self.bundle / "reviewer/responses.json").read_text())
        self.assertFalse(template["author_labels_unseen"])
        self.assertTrue(all(record["label"] is None for record in template["reviews"]))
        with self.assertRaises(FileExistsError):
            prepare_review(self.data, "author", self.bundle)

    def test_complete_agreement_exports_only_reviewed_training_families(self) -> None:
        before = self.data.read_bytes()
        report, output = self.reconcile(self.response)
        self.assertEqual(report["accepted_cases"], 4)
        self.assertEqual(report["accepted_families"], 2)
        reviewed = json.loads((output / "reviewed.json").read_text())
        self.assertTrue(all(case["split"] == "train" for case in reviewed["cases"]))
        self.assertEqual(
            reviewed["review_report_sha256"],
            hashlib.sha256((output / "report.json").read_bytes()).hexdigest(),
        )
        self.assertEqual(self.data.read_bytes(), before)

    def test_one_disagreement_withholds_entire_family(self) -> None:
        self.response["reviews"][0]["label"] = "needs_reply"
        rejected_id = self.manifest["mapping"][self.response["reviews"][0]["id"]]
        source = json.loads(self.data.read_text())["cases"]
        rejected_group = next(
            case["group"] for case in source if case["id"] == rejected_id
        )
        report, output = self.reconcile(self.response)
        self.assertEqual(report["blocked_families"], [rejected_group])
        self.assertEqual(report["accepted_cases"], 2)
        reviewed = json.loads((output / "reviewed.json").read_text())
        self.assertTrue(
            all(case["group"] != rejected_group for case in reviewed["cases"])
        )
        self.assertEqual(
            sum(row["status"] == "disputed" for row in report["decisions"]), 1
        )

    def test_partial_review_never_exports_a_partial_family(self) -> None:
        self.response["reviews"] = self.response["reviews"][:1]
        report, output = self.reconcile(self.response)
        self.assertEqual(report["reviewed"], 1)
        self.assertEqual(report["accepted_cases"], 0)
        self.assertFalse((output / "reviewed.json").exists())

    def test_flags_block_export_even_when_labels_agree(self) -> None:
        for record in self.response["reviews"]:
            record["flags"] = ["policy_mismatch"]
        report, output = self.reconcile(self.response)
        self.assertEqual(report["accepted_cases"], 0)
        self.assertFalse((output / "reviewed.json").exists())

    def test_invalid_attestations_ids_evidence_and_labels_are_rejected(self) -> None:
        mutations = [
            lambda value: value.update(reviewer=" AUTHOR "),
            lambda value: value.update(author_labels_unseen=False),
            lambda value: value.update(packet_sha256="different"),
            lambda value: value["reviews"][0].update(id="unknown"),
            lambda value: value["reviews"][1].update(id=value["reviews"][0]["id"]),
            lambda value: value["reviews"][0].update(label=None),
            lambda value: value["reviews"][0].update(rationale=" "),
            lambda value: value["reviews"][0].update(flags=["invented"]),
            lambda value: value["reviews"][0].update(evidence=[]),
            lambda value: value["reviews"][0]["evidence"][0].update(
                quote="not in the message"
            ),
            lambda value: value["reviews"][0]["evidence"][0].update(message_index=True),
            lambda value: value["reviews"][0]["evidence"][0].update(message_index=1),
        ]
        for mutation in mutations:
            response = copy.deepcopy(self.response)
            mutation(response)
            with self.subTest(response=response), self.assertRaises(ValueError):
                validate_reviews(response, self.packet, "author", self.digest)

    def test_changed_packet_or_author_labels_invalidate_bundle(self) -> None:
        for relative in ("reviewer/packet.json", "author/source.json"):
            path = self.bundle / relative
            original = path.read_bytes()
            path.write_bytes(original + b" ")
            with (
                self.subTest(path=relative),
                self.assertRaisesRegex(ValueError, "digest mismatch"),
            ):
                self.reconcile(self.response)
            path.write_bytes(original)

    def test_source_must_be_synthetic_and_keep_families_in_one_split(self) -> None:
        source = json.loads(self.data.read_text())
        source["provenance"] = "private_mail"
        self.data.write_text(json.dumps(source))
        with self.assertRaisesRegex(ValueError, "fully synthetic"):
            prepare_review(self.data, "author", self.root / "rejected")
        source["provenance"] = "fully_synthetic_assistant_authored"
        source["cases"][-1]["group"] = "first"
        self.data.write_text(json.dumps(source))
        with self.assertRaisesRegex(ValueError, "family crosses"):
            prepare_review(self.data, "author", self.root / "rejected")


if __name__ == "__main__":
    unittest.main()
