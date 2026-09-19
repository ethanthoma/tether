"""Freeze reviewed v2f partitions after label-blind overlap quarantine."""

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
for name in (
    "training",
    "test",
    "separation",
    "overlap-exclusions",
    "diversity-report",
):
    if (root / f"v2f/{name}.json").exists():
        raise FileExistsError(f"v2f/{name}.json already exists")
reports = {}
for name in ("training", "development", "test"):
    path = root / f"v2f/{name}-candidate.json"
    content = path.read_bytes()
    cases = json.loads(content)["cases"]
    assert 1 <= len(cases) <= 2000
    sentences = defaultdict(set)
    contexts = Counter()
    for case in cases:
        directions = "".join("out" if m["outbound"] else "in" for m in case["messages"])
        contexts[f"{case['expected']}:{directions}"] += 1
        for message in case["messages"]:
            for sentence in re.split(r"[.!?\n]+", message["body"]):
                words = re.findall(r"\w+", sentence.lower())
                if len(words) >= 6:
                    sentences[" ".join(words)].add(case["group"])
    repeated = [len(groups) for groups in sentences.values() if len(groups) > 1]
    reports[name] = {
        "source_sha256": hashlib.sha256(content).hexdigest(),
        "cases": len(cases),
        "families": len({c["group"] for c in cases}),
        "labels": dict(sorted(Counter(c["expected"] for c in cases).items())),
        "two_message_cases": sum(len(c["messages"]) == 2 for c in cases),
        "contexts": dict(sorted(contexts.items())),
        "unique_normalized_sentences_at_least_six_words": len(sentences),
        "sentences_repeated_across_families": len(repeated),
        "maximum_families_per_repeated_sentence": max(repeated, default=0),
    }
(root / "v2f/diversity-report.json").write_text(
    json.dumps(
        {
            "method": "Lowercase word tokens; split sentences on .!? or newline; count sentences of at least six words across distinct author families. This is a repetition diagnostic, not proof of semantic diversity.",
            "sources": reports,
        },
        indent=2,
    )
    + "\n"
)

base = json.loads((root / "v2e/training.json").read_text())
train = [case for case in base["cases"] if case["split"] == "train"]
existing_families = {case["group"] for case in train}
for name in ("training", "development", "test"):
    reviewed = json.loads(
        (root / f"reviews/v2f-{name}/reconciled/reviewed.json").read_text()
    )
    assert reviewed["label_policy"] == "reply-triage-v2"
    assert (
        reviewed["review_status"] == "blind_reviewer_agreed_independence_self_attested"
    )
    expected_split = {"training": "train", "development": "dev", "test": "test"}[name]
    if any(case.get("split") != expected_split for case in reviewed["cases"]):
        raise ValueError(f"reviewed {name} contains an incorrect split")
    if name == "training":
        train += reviewed["cases"]
    elif name == "development":
        dev = reviewed["cases"]
    else:
        test = reviewed["cases"]
test_source = reviewed.copy()
assert len(train) <= 2000
partitions = [train, dev, test]
new_families = {
    case["group"] for cases in partitions for case in cases
} - existing_families
historical, historical_sources = [], []
for name in (
    "cases.json",
    "seed.json",
    "synthetic_scenarios.json",
    "scale_scenarios.json",
    "calibration-temperature.json",
    "calibration-selection.json",
    "calibration-audit.json",
    "v2/training.json",
    "v2/test.json",
    "v2b/test.json",
    "reviews/v2d-test/reconciled/reviewed.json",
):
    content = (root / name).read_bytes()
    source = json.loads(content)
    cases = expand_scenarios(source) if "scenarios" in source else source["cases"]
    evaluation = [case for case in cases if case.get("split") != "train"]
    historical_sources.append(
        {
            "path": name,
            "sha256": hashlib.sha256(content).hexdigest(),
            "cases": len(evaluation),
        }
    )
    historical.extend(
        {**case, "group": f"history:{name}:{case.get('group', case['id'])}"}
        for case in evaluation
    )
blocked, collisions = set(), []
indexed = []
for index, cases in enumerate(partitions + [historical]):
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
        if not exact and similarity < 0.65:
            continue
        blocked.update(affected)
        collisions.append(
            {
                "case_ids": [left["id"], right["id"]],
                "exact_normalized_message": exact,
                "trigram_jaccard": similarity,
                "excluded_families": sorted(affected),
            }
        )
train, dev, test = [
    [case for case in cases if case["group"] not in blocked] for cases in partitions
]
separation = check_separation([train, dev, test])
separation["historical_evaluation"] = check_separation(
    [
        [
            case
            for cases in (train, dev, test)
            for case in cases
            if case["group"] in new_families
        ],
        historical,
    ]
)
base["cases"] = train + dev
test_source["cases"] = test
exclusions = {
    "rule": "Exclude all new families participating in exact-message or >=0.65 trigram overlap across partitions; preserve existing training families. Labels and model predictions are not used.",
    "excluded_families": sorted(blocked),
    "collisions": collisions,
    "historical_sources": historical_sources,
}
for name, value in (
    ("training", base),
    ("test", test_source),
    ("separation", separation),
    ("overlap-exclusions", exclusions),
):
    with (root / f"v2f/{name}.json").open("x") as stream:
        stream.write(json.dumps(value, indent=2) + "\n")
print(json.dumps({"diversity": reports, "separation": separation}))
