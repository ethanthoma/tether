import hashlib
import json
import re
import sys
from collections import Counter, defaultdict
from pathlib import Path

sys.path.insert(0, str(Path("experiments/triage").resolve()))
from calibrate import check_separation

root = Path("experiments/triage")
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
    if name == "training":
        train += reviewed["cases"]
    elif name == "development":
        dev = reviewed["cases"]
    else:
        test = reviewed["cases"]
test_source = reviewed.copy()
assert len(train) <= 2000
partitions = [train, dev, test]
blocked, collisions = set(), []
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
assert len(indexed) <= 2400
for offset, (left_index, left, left_messages, left_trigrams) in enumerate(indexed):
    for right_index, right, right_messages, right_trigrams in indexed[offset + 1 :]:
        if left_index == right_index:
            continue
        exact = bool(left_messages & right_messages)
        union = left_trigrams | right_trigrams
        similarity = len(left_trigrams & right_trigrams) / len(union) if union else 0.0
        if not exact and similarity < 0.65:
            continue
        affected = {left["group"], right["group"]} - existing_families
        assert affected
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
base["cases"] = train + dev
test_source["cases"] = test
exclusions = {
    "rule": "Exclude all new families participating in exact-message or >=0.65 trigram overlap across partitions; preserve existing training families. Labels and model predictions are not used.",
    "excluded_families": sorted(blocked),
    "collisions": collisions,
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
