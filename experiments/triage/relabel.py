"""Build a complete v2 training candidate from recorded semantic annotations."""

import argparse
import hashlib
import json
from collections import Counter
from pathlib import Path

from review import LABEL_POLICY, read_json, validate_reviews, write_json
from train import load_cases


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--authoring", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    candidate, audit = build_candidate(args.source, args.authoring)
    args.output.mkdir(parents=True, exist_ok=False)
    write_json(args.output / "candidate.json", candidate)
    write_json(args.output / "audit.json", audit)
    print(
        json.dumps(
            {key: value for key, value in audit.items() if key != "changes"}, indent=2
        )
    )


def build_candidate(source_path: Path, authoring: Path) -> tuple[dict, dict]:
    source = read_json(source_path)
    if source.get("provenance") != "fully_synthetic_assistant_authored":
        raise ValueError("fully synthetic source required")
    cases = load_cases(source_path)
    if len(cases) > 2000 or any(case.get("split") != "train" for case in cases):
        raise ValueError("expected at most 2000 training cases, without held-out cases")
    originals = {case["id"]: case for case in cases}
    manifest = read_json(authoring / "manifest.json")
    source_digest = hashlib.sha256(source_path.read_bytes()).hexdigest()
    if manifest.get("source_sha256") != source_digest:
        raise ValueError("original source digest mismatch")
    mapping = manifest["mapping"]
    if len(mapping) != len(cases) or {
        item["source_id"] for item in mapping.values()
    } != set(originals):
        raise ValueError("mapping must cover every original case exactly once")
    shards = sorted({item["shard"] for item in mapping.values()})
    if not 1 <= len(shards) <= 32 or any(
        type(shard) is not int or not 1 <= shard <= 32 for shard in shards
    ):
        raise ValueError("invalid authoring shards")
    annotations, submissions = {}, []
    for shard in shards:
        packet_path = authoring / f"packet-{shard}.json"
        labels_path = authoring / f"labels-{shard}.json"
        packet, labels = read_json(packet_path), read_json(labels_path)
        if (
            packet.get("version") != 1
            or labels.get("version") != 1
            or packet.get("label_policy") != LABEL_POLICY
            or labels.get("label_policy") != LABEL_POLICY
        ):
            raise ValueError("v2 authoring policy required")
        packet_digest = hashlib.sha256(packet_path.read_bytes()).hexdigest()
        records = validate_reviews(
            {
                "version": 1,
                "packet_sha256": packet_digest,
                "reviewer": labels["author"],
                "author_labels_unseen": True,
                "reviews": labels["cases"],
            },
            packet,
            "original-v1-corpus",
            packet_digest,
        )
        assigned = {
            identifier for identifier, item in mapping.items() if item["shard"] == shard
        }
        if len(packet["cases"]) != len(assigned) or set(records) != assigned:
            raise ValueError("annotations must cover the entire assigned shard")
        for item in packet["cases"]:
            if item["id"] not in assigned:
                raise ValueError("unexpected authoring case")
            source_id = mapping[item["id"]]["source_id"]
            if (
                item["messages"] != originals[source_id]["messages"]
                or source_id in annotations
            ):
                raise ValueError(
                    "authoring must preserve messages and unique source IDs"
                )
            annotations[source_id] = {**records[item["id"]], "author": labels["author"]}
        submissions.append(
            {
                "author": labels["author"],
                "cases": len(records),
                "packet_sha256": packet_digest,
                "labels_sha256": hashlib.sha256(labels_path.read_bytes()).hexdigest(),
            }
        )
    updated, changes = [], []
    for original in cases:
        annotation = annotations[original["id"]]
        case = {
            key: original[key]
            for key in (
                "id",
                "group",
                "split",
                "domain",
                "capability",
                "variant",
                "messages",
            )
            if key in original
        }
        case.update(expected=annotation["label"], author_flags=annotation["flags"])
        case["annotation"] = {
            "author": annotation["author"],
            "rationale": annotation["rationale"],
            "evidence": annotation["evidence"],
            "previous_label": original["expected"],
        }
        updated.append(case)
        if original["expected"] != case["expected"]:
            changes.append(
                {
                    "id": case["id"],
                    "before": original["expected"],
                    "after": case["expected"],
                    "reason": annotation["rationale"],
                }
            )
    candidate = {
        "version": 1,
        "label_policy": LABEL_POLICY,
        "provenance": source["provenance"],
        "review_status": "semantically_relabeled_awaiting_blind_review",
        "source_sha256": source_digest,
        "cases": updated,
    }
    audit = {
        "version": 1,
        "label_policy": LABEL_POLICY,
        "source_sha256": source_digest,
        "cases": len(cases),
        "families": len({case["group"] for case in cases}),
        "changed_labels": len(changes),
        "unchanged_messages": True,
        "labels": dict(sorted(Counter(case["expected"] for case in updated).items())),
        "author_flags": dict(
            sorted(
                Counter(
                    flag for case in updated for flag in case["author_flags"]
                ).items()
            )
        ),
        "submissions": submissions,
        "changes": changes,
    }
    return candidate, audit


if __name__ == "__main__":
    main()
