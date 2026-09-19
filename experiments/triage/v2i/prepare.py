"""Freeze independently reviewed cutoff calibration and test partitions."""

import hashlib
import json
import re
import sys
from collections import Counter, defaultdict
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from build_synthetic import expand_scenarios
from calibrate import check_separation

root = Path(__file__).resolve().parents[1]
outputs = (
    "calibration",
    "test",
    "separation",
    "overlap-exclusions",
    "diversity-report",
)
for name in outputs:
    if (root / f"v2i/{name}.json").exists():
        raise FileExistsError(f"v2i/{name}.json already exists")

documents, diversity = {}, {}
for name, split, count in (("calibration", "dev", 400), ("test", "test", 200)):
    content = (root / f"v2i/{name}-candidate.json").read_bytes()
    authored = json.loads(content)["cases"]
    assert len(authored) == count
    sentences = defaultdict(set)
    for case in authored:
        for message in case["messages"]:
            for sentence in re.split(r"[.!?\n]+", message["body"]):
                words = re.findall(r"\w+", sentence.lower())
                if len(words) >= 6:
                    sentences[" ".join(words)].add(case["group"])
    diversity[name] = {
        "source_sha256": hashlib.sha256(content).hexdigest(),
        "cases": count,
        "families": len({case["group"] for case in authored}),
        "labels": dict(Counter(case["expected"] for case in authored)),
        "contexts": dict(
            Counter(
                case["expected"]
                + ":"
                + "-".join(
                    "out" if message["outbound"] else "in"
                    for message in case["messages"]
                )
                for case in authored
            )
        ),
        "sentences_repeated_across_families": sum(
            len(groups) > 1 for groups in sentences.values()
        ),
    }
    document = json.loads(
        (root / f"v2i/{name}-review/reconciled/reviewed.json").read_text()
    )
    assert document["label_policy"] == "reply-triage-v2"
    assert (
        document["review_status"] == "blind_reviewer_agreed_independence_self_attested"
    )
    if any(case.get("split") != split for case in document["cases"]):
        raise ValueError("reviewed partition contains an incorrect split")
    documents[name] = document

references = [
    source["path"]
    for source in json.loads((root / "v2f/overlap-exclusions.json").read_text())[
        "historical_sources"
    ]
] + ["v2h/training.json", "v2f/test.json"]
historical, sources, seen = [], [], set()
for name in references:
    content = (root / name).read_bytes()
    source = json.loads(content)
    cases = expand_scenarios(source) if "scenarios" in source else source["cases"]
    sources.append(
        {
            "path": name,
            "sha256": hashlib.sha256(content).hexdigest(),
            "cases": len(cases),
        }
    )
    for case in cases:
        key = tuple(
            " ".join(re.findall(r"\w+", message["body"].lower()))
            for message in case["messages"]
        )
        if key not in seen:
            seen.add(key)
            historical.append(
                {**case, "group": f"history:{name}:{case.get('group', case['id'])}"}
            )

partitions = [documents["calibration"]["cases"], documents["test"]["cases"], historical]
new_families = {case["group"] for cases in partitions[:2] for case in cases}
indexed = []
for index, cases in enumerate(partitions):
    for case in cases:
        messages = [
            " ".join(re.findall(r"\w+", message["body"].lower()))
            for message in case["messages"]
        ]
        words = " ".join(messages).split()
        indexed.append(
            (index, case, set(messages), set(zip(words, words[1:], words[2:])))
        )
assert len(indexed) <= 6000
blocked, collisions = set(), []
for offset, (left_index, left, left_messages, left_trigrams) in enumerate(indexed):
    for right_index, right, right_messages, right_trigrams in indexed[offset + 1 :]:
        if left_index == right_index:
            continue
        affected = {left["group"], right["group"]} & new_families
        if not affected:
            continue
        exact = bool(left_messages & right_messages)
        union = left_trigrams | right_trigrams
        similarity = len(left_trigrams & right_trigrams) / len(union) if union else 0.0
        if exact or similarity >= 0.65:
            blocked.update(affected)
            collisions.append(
                {
                    "case_ids": [left["id"], right["id"]],
                    "exact_normalized_message": exact,
                    "trigram_jaccard": similarity,
                    "excluded_families": sorted(affected),
                }
            )
for document in documents.values():
    document["cases"] = [
        case for case in document["cases"] if case["group"] not in blocked
    ]
calibration, test = documents["calibration"]["cases"], documents["test"]["cases"]
if len(calibration) < 300 or len({case["group"] for case in calibration}) < 150:
    raise ValueError("insufficient reviewed calibration data after quarantine")
if len(test) < 80 or len({case["group"] for case in test}) < 20:
    raise ValueError("insufficient reviewed test data after quarantine")
separation = check_separation([calibration, test, historical])
exclusions = {
    "rule": "Exclude whole new families for exact normalized message or >=0.65 trigram overlap across new partitions or prior training/evaluation; labels and predictions are not used.",
    "historical_sources": sources,
    "historical_unique_message_sequences": len(historical),
    "excluded_families": sorted(blocked),
    "collisions": collisions,
}
for name, value in (
    ("calibration", documents["calibration"]),
    ("test", documents["test"]),
    ("separation", separation),
    ("overlap-exclusions", exclusions),
    ("diversity-report", diversity),
):
    with (root / f"v2i/{name}.json").open("x") as stream:
        stream.write(json.dumps(value, indent=2) + "\n")
print(
    json.dumps(
        {
            "separation": separation,
            "excluded_families": sorted(blocked),
            "diversity": diversity,
        }
    )
)
