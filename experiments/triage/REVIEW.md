# Blind synthetic-label review

This workflow prepares **training data only** for a separate human or model
reviewer. It does not call an LLM, send messages, train a model, or change
production. Existing model reports and held-out labels stay unchanged.

[LABELING.md](LABELING.md) defines the current experimental labels, outstanding
obligations, cancellations, promises, mixed ownership, and the boundary with
production policy. Read it before authoring the next synthetic batch.

## Prepare a review

Use the Python environment in [TRAINING.md](TRAINING.md). The input is an exported
case dataset, not the scenario-authoring format. For the current expanded corpus:

```sh
nix-shell experiments/triage/shell.nix
experiments/triage/.venv/bin/python experiments/triage/review.py prepare \
  --data experiments/triage/runs/scaling-v1/full.json \
  --author synthetic-corpus-author \
  --output experiments/triage/runs/review-v1
```

Only the 981 training cases from 148 families enter the packet. Development and
test cases are excluded. A family crossing source splits is rejected. Source
provenance must declare fully synthetic content; that declaration is not a
semantic or provenance audit.

The fresh output directory contains:

- `reviewer/packet.json`: a snapshot of the specification and randomly shuffled
  messages with opaque random IDs. Author labels, evidence, scenario names,
  capabilities, family IDs, split metadata, and predictions are omitted.
- `reviewer/responses.json`: an unfilled response template tied to the packet hash.
- `author/`: the original training cases, private ID mapping, author identity,
  and integrity hashes. Keep this directory away from the blind reviewer.

Give the separate reviewer **only the reviewer directory**. For a model reviewer,
use a fresh context without authoring history and restrict its task to the blinded
files. Shared-workspace agents retain technical filesystem access; this restriction
is an instruction, not an operating-system sandbox. Keep provider,
model/version or human identity in the `reviewer` field; record meaningful provenance,
not merely a different name for the author. No local LLM is required by this tool.

## Collect judgments

The reviewer fills the response template with an identity, the
`author_labels_unseen: true` attestation, and each case's label, rationale, flags,
and evidence. For example, an evidence entry is:

```json
{"message_index": 0, "quote": "copy an exact span from this message"}
```

Message indices are zero-based. Quotes must match the indicated message exactly.
Use only the five specified labels and documented flags. Do not fill answers
from the author's labels or a model's existing predictions.

For a partial review, remove unfinished records from the response array; do not
submit placeholder null labels. Missing records remain pending. Agreement with
`abstain` is valid. Add flags when adjudication is needed, rather than flagging
every deliberately ambiguous example merely because its correct label is abstain.

## Reconcile and resolve

```sh
experiments/triage/.venv/bin/python experiments/triage/review.py reconcile \
  --bundle experiments/triage/runs/review-v1 \
  --responses experiments/triage/runs/review-v1/reviewer/responses.json \
  --output experiments/triage/runs/review-v1-result
```

The command validates identity/attestation, IDs, hashes, labels, rationales, and
evidence before creating output. It writes `report.json` with agreement,
disagreement, pending cases, flags, and blocked families. It writes `reviewed.json`
only if at least one **whole family** has complete, unflagged agreement.

For disjoint reviewer shards, pass multiple paths after `--responses`. Each file
must identify its reviewer and reference the original full packet hash. Every
record retains its reviewer identity in the report. Duplicate IDs across files
are rejected; this combines coverage, not multiple votes on the same case.

A single disputed, flagged, or missing variant withholds its entire family,
including direction reversals. Original labels are never silently replaced.
The report explicitly records that reviewer independence is self-attested;
a different identity does not prove independent reasoning or human validation.
Matching labels can still be wrong.

Resolve disagreements by reviewing the reasoning and clarifying the specification
or correcting the authored scenario. Preserve the old bundle as an audit trail,
then export and obtain a fresh blind review. Do not remove inconvenient variants
to turn a disputed family into an accepted one. No override or majority-vote
shortcut is provided.

The reviewed export contains training cases only. Combine it with separately
reserved development data using the existing split and overlap checks before
training. This is an opt-in experimental export gate; older reproduction commands
can still train their explicitly provisional datasets.

## Current status

The [completed first blind pass](reviews/v1/README.md) covers all 981 training cases
across 148 families using three fresh model contexts. Author/reviewer labels agree
on 979 cases; 336 cases carry review flags. The family gate retains 109 cases from
28 families, but this subset lacks abstain examples and must not replace training
data. The committed audit includes original judgments and reproducible inputs.
Scratch run directories remain ignored. Resolve the policy and context flags before
further training; fresh evaluation families remain separate.
