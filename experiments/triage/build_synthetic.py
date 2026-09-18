"""Validate authored synthetic scenarios and export isolated dataset splits."""

import argparse
import hashlib
import json
import re
from collections import Counter
from pathlib import Path

from train import LABELS, ROOT, load_cases, training_split

STATES = {"recipient", "sender", "none", "bulk", "both", "unknown"}
SPLITS = {"train", "dev", "test"}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--source", type=Path, default=ROOT / "synthetic_scenarios.json"
    )
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if not args.source.is_file() or args.source.stat().st_size > 4 * 1024 * 1024:
        raise ValueError("source must be a regular file of at most 4 MiB")
    source = json.loads(args.source.read_text())
    cases = expand_scenarios(source)
    audit = audit_cases(cases)
    audit["source_sha256"] = hashlib.sha256(args.source.read_bytes()).hexdigest()
    args.output.mkdir(parents=True, exist_ok=False)
    for filename, splits in [
        ("data.json", {"train", "dev"}),
        ("holdout.json", {"test"}),
    ]:
        payload = {
            "version": 1,
            "provenance": "fully_synthetic_assistant_authored",
            "review_status": "single_author_self_reviewed_not_independently_validated",
            "cases": [case for case in cases if case["split"] in splits],
        }
        path = args.output / filename
        path.write_text(json.dumps(payload, indent=2) + "\n")
        load_cases(path)
    (args.output / "audit.json").write_text(json.dumps(audit, indent=2) + "\n")
    print(json.dumps(audit, indent=2))


def obligation_label(state: str, outbound: bool) -> str:
    if state not in STATES:
        raise ValueError("unknown obligation state")
    if state in {"both", "unknown"}:
        return "abstain"
    if state == "none":
        return "fyi"
    if state == "bulk":
        return "noise"
    user_owes = outbound if state == "sender" else not outbound
    return "needs_reply" if user_owes else "waiting_on_them"


def expand_scenarios(source: dict) -> list[dict]:
    if source["version"] != 1:
        raise ValueError("unsupported scenario schema")
    if source["provenance"] != "fully_synthetic_assistant_authored":
        raise ValueError("synthetic provenance required")
    scenarios = source["scenarios"]
    if not 1 <= len(scenarios) <= 500:
        raise ValueError("expected 1–500 scenario families")
    identifiers: set[str] = set()
    cases = []
    for scenario in scenarios:
        group = scenario["id"]
        if not re.fullmatch(r"[a-z][a-z0-9_]{0,79}", group) or group in identifiers:
            raise ValueError("unique snake_case scenario IDs required")
        identifiers.add(group)
        if scenario["split"] not in SPLITS:
            raise ValueError("invalid scenario split")
        if type(scenario["mirror"]) is not bool:
            raise ValueError("mirror must be boolean")
        if not scenario["domain"] or not scenario["capability"]:
            raise ValueError("domain and capability are required")
        variants = scenario["variants"]
        if not 2 <= len(variants) <= 6:
            raise ValueError("expected 2–6 contrasting variants per family")
        if len({variant["state"] for variant in variants}) < 2:
            raise ValueError("each family must contrast obligation states")
        for index, variant in enumerate(variants):
            messages = variant["messages"]
            if not 1 <= len(messages) <= 2:
                raise ValueError("expected one or two chronological messages")
            for message in messages:
                if type(message["outbound"]) is not bool or not isinstance(
                    message["body"], str
                ):
                    raise ValueError("invalid message")
                if not 1 <= len(message["body"].encode()) <= 4000:
                    raise ValueError("invalid body size")
            evidence = variant["evidence"]
            if not evidence or not any(
                evidence in message["body"] for message in messages
            ):
                raise ValueError(
                    f"{group}/{index}: evidence must quote a visible message"
                )
            for mirrored in [False, True] if scenario["mirror"] else [False]:
                transformed = [
                    {"outbound": m["outbound"] != mirrored, "body": m["body"]}
                    for m in messages
                ]
                cases.append(
                    {
                        "id": f"{group}_{index}" + ("_reversed" if mirrored else ""),
                        "group": group,
                        "split": scenario["split"],
                        "domain": scenario["domain"],
                        "capability": scenario["capability"],
                        "variant": "direction_reversed" if mirrored else "authored",
                        "expected": obligation_label(
                            variant["state"], transformed[-1]["outbound"]
                        ),
                        "obligation_state": variant["state"],
                        "evidence": evidence,
                        "messages": transformed,
                    }
                )
    return cases


def audit_cases(cases: list[dict]) -> dict:
    if not 1 <= len(cases) <= 6000:
        raise ValueError("expected 1–6000 expanded cases")
    training_split([case for case in cases if case["split"] != "test"])
    groups: dict[str, str] = {}
    bodies: dict[str, str] = {}
    threads: dict[str, str] = {}
    shingle_texts: set[str] = set()
    shingled: list[tuple[dict, set[tuple[str, ...]]]] = []
    for case in cases:
        split = case["split"]
        if groups.setdefault(case["group"], split) != split:
            raise ValueError("scenario family crosses splits")
        for message in case["messages"]:
            normalized = " ".join(re.findall(r"\w+", message["body"].lower()))
            if bodies.setdefault(normalized, split) != split:
                raise ValueError("normalized message crosses splits")
        normalized_thread = json.dumps(case["messages"], sort_keys=True)
        if normalized_thread in threads:
            raise ValueError("duplicate thread")
        threads[normalized_thread] = case["id"]
        words = re.findall(
            r"\w+", " ".join(m["body"] for m in case["messages"]).lower()
        )
        normalized_text = " ".join(words)
        if normalized_text not in shingle_texts:
            shingle_texts.add(normalized_text)
            shingled.append((case, set(zip(words, words[1:], words[2:]))))
    maximum = 0.0
    nearest = []
    for index, (left, left_shingles) in enumerate(shingled):
        for right, right_shingles in shingled[index + 1 :]:
            if left["split"] == right["split"]:
                continue
            union = left_shingles | right_shingles
            similarity = (
                len(left_shingles & right_shingles) / len(union) if union else 0
            )
            if similarity > maximum:
                maximum, nearest = similarity, [left["id"], right["id"]]
            if similarity >= 0.65:
                raise ValueError(
                    f"near-duplicate crosses splits: {left['id']} / {right['id']}"
                )
    report: dict = {
        "status": "structural_checks_passed_semantic_labels_provisional",
        "cases": len(cases),
        "families": len(groups),
        "authored_variants": sum(case["variant"] == "authored" for case in cases),
        "unique_thread_texts": len(shingled),
        "cross_split_trigram_jaccard_max": round(maximum, 4),
        "nearest_cross_split_pair": nearest,
        "splits": {},
    }
    for split in sorted(SPLITS):
        rows = [case for case in cases if case["split"] == split]
        counts = Counter(case["expected"] for case in rows)
        if set(counts) != set(LABELS):
            raise ValueError("all five labels required in every split")
        report["splits"][split] = {
            "cases": len(rows),
            "families": len({case["group"] for case in rows}),
            "labels": dict(sorted(counts.items())),
            "domains": dict(sorted(Counter(case["domain"] for case in rows).items())),
            "capabilities": dict(
                sorted(Counter(case["capability"] for case in rows).items())
            ),
        }
    return report


if __name__ == "__main__":
    main()
