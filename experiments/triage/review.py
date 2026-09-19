"""Prepare blind label review and quarantine unresolved scenario families."""

import argparse
import hashlib
import json
import secrets
from pathlib import Path
from random import SystemRandom

from train import LABELS, ROOT, load_cases

LABEL_POLICY = "reply-triage-v2"
FLAGS = {
    "policy_mismatch",
    "insufficient_context",
    "unnatural_direction",
    "label_ambiguity",
}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    prepare = commands.add_parser("prepare")
    prepare.add_argument("--data", type=Path, required=True)
    prepare.add_argument("--author", required=True)
    prepare.add_argument("--output", type=Path, required=True)
    prepare.add_argument(
        "--splits", nargs="+", choices=("train", "dev", "test"), default=["train"]
    )
    reconcile = commands.add_parser("reconcile")
    reconcile.add_argument("--bundle", type=Path, required=True)
    reconcile.add_argument("--responses", type=Path, nargs="+", required=True)
    reconcile.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if args.command == "prepare":
        prepare_review(args.data, args.author, args.output, tuple(args.splits))
    else:
        reconcile_review(args.bundle, args.responses, args.output)


def read_json(path: Path) -> dict:
    if not path.is_file() or path.stat().st_size > 16 * 1024 * 1024:
        raise ValueError("review file must be a regular file of at most 16 MiB")
    value = json.loads(path.read_text())
    if not isinstance(value, dict):
        raise TypeError("review file must contain an object")
    return value


def write_json(path: Path, value: dict) -> None:
    with path.open("x") as stream:
        stream.write(json.dumps(value, indent=2) + "\n")


def identity(value: str) -> str:
    if not isinstance(value, str) or not 1 <= len(value.strip()) <= 200:
        raise ValueError("nonempty identity of at most 200 characters required")
    return value.strip().casefold()


def prepare_review(
    data: Path, author: str, output: Path, splits: tuple[str, ...] = ("train",)
) -> None:
    identity(author)
    if (
        not splits
        or len(set(splits)) != len(splits)
        or not set(splits) <= {"train", "dev", "test"}
    ):
        raise ValueError("select distinct train, dev, or test splits")
    source = read_json(data)
    if source.get("provenance") != "fully_synthetic_assistant_authored":
        raise ValueError("fully synthetic source required")
    if source.get("label_policy") != LABEL_POLICY:
        raise ValueError(
            f"source label_policy must be {LABEL_POLICY}; relabel before review"
        )
    all_cases = load_cases(data)
    partitions = {}
    for case in all_cases:
        split, group = case.get("split"), case.get("group")
        if (
            split not in ("train", "dev", "test")
            or not isinstance(group, str)
            or not group
        ):
            raise ValueError("each source case needs a split and family")
        if partitions.setdefault(group, split) != split:
            raise ValueError("family crosses source splits")
    cases = [case for case in all_cases if case["split"] in splits]
    if not 1 <= len(cases) <= 2000:
        raise ValueError("review requires 1–2000 selected cases")
    for case in cases:
        if (
            case.get("expected") not in LABELS
            or not isinstance(case.get("group"), str)
            or not case["group"]
        ):
            raise ValueError("each selected case needs a label and family")
    SystemRandom().shuffle(cases)
    mapping = {}
    blinded = []
    for case in cases:
        identifier = secrets.token_hex(16)
        if identifier in mapping:
            raise RuntimeError("review ID collision")
        mapping[identifier] = case["id"]
        blinded.append(
            {
                "id": identifier,
                "messages": [
                    {"outbound": message["outbound"], "body": message["body"]}
                    for message in case["messages"]
                ],
            }
        )
    output.mkdir(parents=True, exist_ok=False)
    (output / "reviewer").mkdir()
    (output / "author").mkdir(mode=0o700)
    packet = {
        "version": 1,
        "label_policy": LABEL_POLICY,
        "specification": (ROOT / "LABELING.md").read_text(),
        "cases": blinded,
    }
    packet_path = output / "reviewer/packet.json"
    write_json(packet_path, packet)
    digest = hashlib.sha256(packet_path.read_bytes()).hexdigest()
    write_json(
        output / "reviewer/responses.json",
        {
            "version": 1,
            "packet_sha256": digest,
            "reviewer": "",
            "author_labels_unseen": False,
            "reviews": [
                {
                    "id": case["id"],
                    "label": None,
                    "evidence": [],
                    "rationale": "",
                    "flags": [],
                }
                for case in blinded
            ],
        },
    )
    write_json(
        output / "author/source.json",
        {
            "version": 1,
            "provenance": source["provenance"],
            "label_policy": LABEL_POLICY,
            "cases": cases,
        },
    )
    write_json(
        output / "author/manifest.json",
        {
            "version": 1,
            "author": author,
            "input_sha256": hashlib.sha256(data.read_bytes()).hexdigest(),
            "source_sha256": hashlib.sha256(
                (output / "author/source.json").read_bytes()
            ).hexdigest(),
            "packet_sha256": digest,
            "mapping": mapping,
        },
    )
    print(
        json.dumps(
            {
                "status": "awaiting_independent_review",
                "cases": len(cases),
                "families": len({case["group"] for case in cases}),
            }
        )
    )


def validate_reviews(
    response: dict, packet: dict, author: str, digest: str
) -> dict[str, dict]:
    if response.get("version") != 1 or response.get("packet_sha256") != digest:
        raise ValueError("response version or packet digest mismatch")
    if identity(response.get("reviewer")) == identity(author):
        raise ValueError("reviewer must differ from the author")
    if response.get("author_labels_unseen") is not True:
        raise ValueError("blind review attestation required")
    cases = {case["id"]: case for case in packet["cases"]}
    records = response.get("reviews")
    if not isinstance(records, list) or len(records) > len(cases):
        raise ValueError("invalid review count")
    reviews = {}
    for record in records:
        if not isinstance(record, dict):
            raise TypeError("review must be an object")
        identifier = record.get("id")
        if (
            not isinstance(identifier, str)
            or identifier not in cases
            or identifier in reviews
        ):
            raise ValueError("unknown or duplicate review ID")
        if record.get("label") not in LABELS:
            raise ValueError("valid review label required")
        rationale = record.get("rationale")
        if not isinstance(rationale, str) or not 1 <= len(rationale.strip()) <= 2000:
            raise ValueError("review rationale must contain 1–2000 characters")
        flags = record.get("flags")
        if (
            not isinstance(flags, list)
            or len(flags) > len(FLAGS)
            or any(not isinstance(flag, str) or flag not in FLAGS for flag in flags)
        ):
            raise ValueError("unknown review flags")
        evidence = record.get("evidence")
        if not isinstance(evidence, list) or not 1 <= len(evidence) <= 8:
            raise ValueError("review requires 1–8 evidence spans")
        messages = cases[identifier]["messages"]
        for span in evidence:
            if not isinstance(span, dict):
                raise TypeError("evidence must be an object")
            index, quote = span.get("message_index"), span.get("quote")
            if type(index) is not int or not 0 <= index < len(messages):
                raise ValueError("evidence message index out of bounds")
            if (
                not isinstance(quote, str)
                or not quote.strip()
                or quote not in messages[index]["body"]
            ):
                raise ValueError("evidence must quote the indicated message")
        reviews[identifier] = record
    return reviews


def reconcile_review(bundle: Path, responses: list[Path], output: Path) -> None:
    if not 1 <= len(responses) <= 32:
        raise ValueError("expected 1–32 response files")
    manifest = read_json(bundle / "author/manifest.json")
    packet_path = bundle / "reviewer/packet.json"
    packet = read_json(packet_path)
    digest = hashlib.sha256(packet_path.read_bytes()).hexdigest()
    source_path = bundle / "author/source.json"
    source = read_json(source_path)
    if source.get("label_policy") != packet.get("label_policy"):
        raise ValueError("source and packet label policies differ")
    policy_metadata = {key: source[key] for key in ("label_policy",) if key in source}
    if (
        manifest.get("version") != 1
        or packet.get("version") != 1
        or digest != manifest["packet_sha256"]
    ):
        raise ValueError("bundle version or packet digest mismatch")
    if (
        hashlib.sha256(source_path.read_bytes()).hexdigest()
        != manifest["source_sha256"]
    ):
        raise ValueError("author source digest mismatch")
    cases = load_cases(source_path)
    mapping = manifest["mapping"]
    if (
        len(mapping) != len(cases)
        or len(packet["cases"]) != len(cases)
        or set(mapping.values()) != {case["id"] for case in cases}
    ):
        raise ValueError("incomplete review mapping")
    originals = {case["id"]: case for case in cases}
    packet_ids = set()
    for case in packet["cases"]:
        identifier = case["id"]
        if identifier not in mapping or identifier in packet_ids:
            raise ValueError("invalid packet IDs")
        packet_ids.add(identifier)
        expected_messages = [
            {"outbound": message["outbound"], "body": message["body"]}
            for message in originals[mapping[identifier]]["messages"]
        ]
        if case["messages"] != expected_messages:
            raise ValueError("packet messages differ from author source")
    reviews, submissions = {}, []
    for path in responses:
        response = read_json(path)
        submitted = validate_reviews(response, packet, manifest["author"], digest)
        if reviews.keys() & submitted.keys():
            raise ValueError("case reviewed more than once across response files")
        for identifier, record in submitted.items():
            reviews[identifier] = {**record, "reviewer": response["reviewer"]}
        submissions.append(
            {
                "reviewer": response["reviewer"],
                "responses_sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                "cases": len(submitted),
            }
        )
    decisions, blocked = [], set()
    for identifier, source_id in mapping.items():
        case = originals[source_id]
        review = reviews.get(identifier)
        author_flags = case.get("author_flags", [])
        if (
            not isinstance(author_flags, list)
            or len(author_flags) > len(FLAGS)
            or any(
                not isinstance(flag, str) or flag not in FLAGS for flag in author_flags
            )
        ):
            raise ValueError("unknown author flags")
        status = "agreed"
        if review is None:
            status = "pending"
        elif review["label"] != case["expected"] or review["flags"] or author_flags:
            status = "disputed"
        if status != "agreed":
            blocked.add(case["group"])
        decisions.append(
            {
                "id": source_id,
                "review_id": identifier,
                "group": case["group"],
                "author_label": case["expected"],
                **({"author_flags": author_flags} if "author_flags" in case else {}),
                "status": status,
                "review": review,
            }
        )
    accepted = [case for case in cases if case["group"] not in blocked]
    report = {
        "version": 1,
        **policy_metadata,
        "packet_sha256": digest,
        "submissions": submissions,
        "author": manifest["author"],
        "independence": "reviewer_attested_not_externally_verified",
        "cases": len(cases),
        "reviewed": len(reviews),
        "accepted_cases": len(accepted),
        "accepted_families": len({case["group"] for case in accepted}),
        "blocked_families": sorted(blocked),
        "decisions": decisions,
    }
    output.mkdir(parents=True, exist_ok=False)
    write_json(output / "report.json", report)
    if accepted:
        write_json(
            output / "reviewed.json",
            {
                "version": 1,
                "provenance": source["provenance"],
                **policy_metadata,
                "review_status": "blind_reviewer_agreed_independence_self_attested",
                "review_report_sha256": hashlib.sha256(
                    (output / "report.json").read_bytes()
                ).hexdigest(),
                "cases": accepted,
            },
        )
    print(
        json.dumps(
            {
                key: value
                for key, value in report.items()
                if key not in {"decisions", "blocked_families"}
            }
        )
    )


if __name__ == "__main__":
    main()
