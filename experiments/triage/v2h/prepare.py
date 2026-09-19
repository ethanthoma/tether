import hashlib
import json
import re
import sys
from collections import Counter
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from build_synthetic import expand_scenarios
from calibrate import check_separation

root = Path(__file__).resolve().parents[1]
for name in ("training", "role-training-filtered", "role-separation", "separation"):
    if (root / f"v2h/{name}.json").exists():
        raise FileExistsError(f"v2h/{name}.json already exists")
source = json.loads((root / "v2h/training-review/reconciled/reviewed.json").read_text())
references = json.loads((root / "v2f/overlap-exclusions.json").read_text())[
    "historical_sources"
]
for name in ("v2f/training.json", "v2f/test.json"):
    references.append(
        {"path": name, "sha256": hashlib.sha256((root / name).read_bytes()).hexdigest()}
    )
evaluation = []
for reference in references:
    content = (root / reference["path"]).read_bytes()
    assert hashlib.sha256(content).hexdigest() == reference["sha256"]
    data = json.loads(content)
    cases = expand_scenarios(data) if "scenarios" in data else data["cases"]
    evaluation.extend(case for case in cases if case.get("split") != "train")
assert len(evaluation) <= 2000
assert len(source["cases"]) == 200
blocked, collisions = set(), []
indexed = []
for case in evaluation:
    messages = {
        " ".join(re.findall(r"\w+", message["body"].lower()))
        for message in case["messages"]
    }
    words = re.findall(
        r"\w+", " ".join(message["body"] for message in case["messages"]).lower()
    )
    indexed.append((case["id"], messages, set(zip(words, words[1:], words[2:]))))
for case in source["cases"]:
    assert case["split"] == "train"
    messages = {
        " ".join(re.findall(r"\w+", message["body"].lower()))
        for message in case["messages"]
    }
    words = re.findall(
        r"\w+", " ".join(message["body"] for message in case["messages"]).lower()
    )
    trigrams = set(zip(words, words[1:], words[2:]))
    for reference_id, reference_messages, reference_trigrams in indexed:
        exact = bool(messages & reference_messages)
        union = trigrams | reference_trigrams
        similarity = len(trigrams & reference_trigrams) / len(union) if union else 0.0
        if exact or similarity >= 0.65:
            blocked.add(case["group"])
            collisions.append(
                {
                    "case_id": case["id"],
                    "reference_id": reference_id,
                    "exact_message": exact,
                    "trigram_jaccard": similarity,
                }
            )
source["cases"] = [case for case in source["cases"] if case["group"] not in blocked]
report = {
    "rule": "Exclude entire supplemental families for exact normalized message or >=0.65 trigram overlap with current or historical evaluation; labels/predictions not used.",
    "references": references,
    "excluded_families": sorted(blocked),
    "collisions": collisions,
    "retained": len(source["cases"]),
    "labels": dict(Counter(case["expected"] for case in source["cases"])),
    "separation": check_separation([source["cases"], evaluation]),
}
for name, value in (("role-training-filtered", source), ("role-separation", report)):
    with (root / f"v2h/{name}.json").open("x") as stream:
        stream.write(json.dumps(value, indent=2) + "\n")
print(
    json.dumps(
        {
            key: value
            for key, value in report.items()
            if key not in {"references", "collisions"}
        }
    )
)

base = json.loads((root / "v2f/training.json").read_text())
train = [case for case in base["cases"] if case["split"] == "train"]
dev = [case for case in base["cases"] if case["split"] == "dev"]
test = json.loads((root / "v2f/test.json").read_text())["cases"]
assert len(train) == 1989
assert len(dev) == 152
assert len(test) == 154
combined = train + source["cases"] + dev
assert len({case["id"] for case in combined}) == len(combined)
assert not {case["group"] for case in train} & {
    case["group"] for case in source["cases"]
}
base["cases"] = combined
separation = check_separation([train + source["cases"], dev, test])
for name, value in (("training", base), ("separation", separation)):
    with (root / f"v2h/{name}.json").open("x") as stream:
        stream.write(json.dumps(value, indent=2) + "\n")
print(json.dumps(separation))
