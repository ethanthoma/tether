import copy
import hashlib
import json
import tempfile
import unittest
from pathlib import Path

from relabel import build_candidate


class RelabelTests(unittest.TestCase):
    def setUp(self) -> None:
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.source = self.root / "source.json"
        self.cases = [
            {
                "id": "first",
                "group": "family",
                "split": "train",
                "expected": "needs_reply",
                "obligation_state": "sender",
                "evidence": "old evidence",
                "messages": [
                    {"outbound": True, "body": "I'll carry the boxes upstairs."}
                ],
            },
            {
                "id": "second",
                "group": "family",
                "split": "train",
                "expected": "abstain",
                "messages": [
                    {"outbound": False, "body": "Is the arrangement happening?"}
                ],
            },
        ]
        self.source.write_text(
            json.dumps(
                {
                    "version": 1,
                    "provenance": "fully_synthetic_assistant_authored",
                    "cases": self.cases,
                }
            )
        )
        self.manifest = {
            "source_sha256": hashlib.sha256(self.source.read_bytes()).hexdigest(),
            "mapping": {
                "opaque-first": {"source_id": "first", "shard": 1},
                "opaque-second": {"source_id": "second", "shard": 1},
            },
        }
        self.packet = {
            "version": 1,
            "label_policy": "reply-triage-v2",
            "specification": "test specification",
            "cases": [
                {"id": f"opaque-{case['id']}", "messages": case["messages"]}
                for case in self.cases
            ],
        }
        self.labels = {
            "version": 1,
            "label_policy": "reply-triage-v2",
            "author": "v2-author",
            "cases": [
                {
                    "id": "opaque-first",
                    "label": "abstain",
                    "evidence": [
                        {
                            "message_index": 0,
                            "quote": self.cases[0]["messages"][0]["body"],
                        }
                    ],
                    "rationale": "User task commitment needs separate routing.",
                    "flags": [],
                },
                {
                    "id": "opaque-second",
                    "label": "needs_reply",
                    "evidence": [
                        {"message_index": 0, "quote": "Is the arrangement happening?"}
                    ],
                    "rationale": "The visible question requests an answer.",
                    "flags": ["label_ambiguity"],
                },
            ],
        }
        self.write_authoring()

    def write_authoring(self) -> None:
        for name, value in (
            ("manifest.json", self.manifest),
            ("packet-1.json", self.packet),
            ("labels-1.json", self.labels),
        ):
            (self.root / name).write_text(json.dumps(value))

    def test_semantic_annotations_replace_labels_without_changing_messages(
        self,
    ) -> None:
        original = self.source.read_bytes()
        candidate, audit = build_candidate(self.source, self.root)
        self.assertEqual(candidate["label_policy"], "reply-triage-v2")
        self.assertEqual(audit["changed_labels"], 2)
        self.assertEqual(audit["families"], 1)
        self.assertEqual(
            [case["expected"] for case in candidate["cases"]],
            ["abstain", "needs_reply"],
        )
        for before, after in zip(self.cases, candidate["cases"], strict=True):
            self.assertEqual(before["messages"], after["messages"])
            self.assertEqual(before["group"], after["group"])
            self.assertEqual(after["annotation"]["previous_label"], before["expected"])
            self.assertNotIn("obligation_state", after)
            self.assertNotIn("evidence", after)
        self.assertEqual(candidate["cases"][1]["author_flags"], ["label_ambiguity"])
        self.assertEqual(self.source.read_bytes(), original)
        self.assertEqual(build_candidate(self.source, self.root), (candidate, audit))

    def test_missing_annotations_wrong_policy_and_changed_messages_fail_closed(
        self,
    ) -> None:
        packet, labels = copy.deepcopy(self.packet), copy.deepcopy(self.labels)
        for mutation in ("missing", "policy", "messages", "evidence"):
            self.packet, self.labels = copy.deepcopy(packet), copy.deepcopy(labels)
            if mutation == "missing":
                self.labels["cases"].pop()
            elif mutation == "policy":
                self.labels["label_policy"] = "v1"
            elif mutation == "messages":
                self.packet["cases"][0]["messages"][0]["body"] += " Changed."
            else:
                self.labels["cases"][0]["evidence"][0]["quote"] = "invented"
            self.write_authoring()
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                build_candidate(self.source, self.root)

    def test_held_out_cases_and_incomplete_mapping_are_rejected(self) -> None:
        self.manifest["mapping"].pop("opaque-second")
        self.write_authoring()
        with self.assertRaisesRegex(ValueError, "every original case"):
            build_candidate(self.source, self.root)
        source = json.loads(self.source.read_text())
        source["cases"][0]["split"] = "test"
        self.source.write_text(json.dumps(source))
        with self.assertRaisesRegex(ValueError, "held-out"):
            build_candidate(self.source, self.root)


if __name__ == "__main__":
    unittest.main()
