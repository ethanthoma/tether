# Blind review v1: completed

Three OpenAI Codex subagents each reviewed 327 disjoint training cases in fresh
contexts (`fork_turns=none`), with inherited model settings and no model override.
Each received only its shuffled packet shard and the labeling specification. They
were instructed not to read the repository, author labels, other shards, or model
predictions, and all attested that author labels remained unseen. File restrictions
were instructions, not an OS sandbox. This establishes a separate-context model
review, not human validation, multiple votes per case, or model diversity.

## Findings

| Measure | Count |
| --- | ---: |
| Reviewed cases / training families | 981 / 148 |
| Author/reviewer label agreement | 979 |
| Label disagreements | 2 |
| Cases with reviewer flags | 336 |
| Unflagged label agreements | 643 |
| Policy-mismatch flags | 231 |
| Insufficient-context flags | 104 |
| Unnatural-direction flags | 1 |
| Whole families withheld | 120 |
| Whole families / cases passing the export gate | 28 / 109 |

The 109-case export contains 30 FYI, 30 needs-reply, 44 noise, and five
waiting-on-them examples. **It has no abstain examples and is not ready for
five-label training.** It is an audit output, not a promoted replacement dataset.
No original labels, model weights, thresholds, or production behavior changed.

The largest issue is the meaning of an obligation: reviewers flagged user tasks
and promises that the experiment labels `needs_reply`, while production usually
requires a requested prose response. The 104 context flags also need adjudication:
many accompany agreement on `abstain`. Correctly ambiguous examples are valuable
training data; these flags are reviewer judgments, not proof that the examples
must be discarded. The gate preserves them in the blocked-family audit.

The two label disagreements are `locker_location_2_reversed` and
`ceramics_booking_2_reversed`. Their authors chose abstain for unclear context;
the reviewer chose waiting-on-them because each message explicitly asks a question.
Both original-direction counterparts received abstain. These are the only two
direction-label inconsistencies among 437 paired cases. Clarify whether a
visible question establishes a reply obligation despite a missing referent before
changing either side; do not automatically prefer author or reviewer.

The unnatural-direction flag concerns `scale_course_transfer_0_reversed`: a
user-sent institutional enrollment notice. The family remains withheld.

## Audit contents

- `bundle/reviewer/packet.json`: exact full blinded input and specification snapshot.
- `bundle/author/`: original training cases, ID mapping, and hashes. These were
  not included in reviewer inputs.
- `responses/1.json`, `2.json`, `3.json`: original reviewer submissions.
- `result/report.json`: per-case comparison, reviewer attribution, and withheld families.
- `result/reviewed.json`: the 109-case family-gated export.
- `summary.json`: counts, provenance, hash linkage, and direction inconsistencies.

The archived packet order defines the shards: offsets 0–326, 327–653, and 654–980.
Every submission was checked against its assigned IDs as well as the evidence,
label, and full-packet-hash validators. All 981 cases occur exactly once.
Messages and judgments are synthetic; this curated audit is committed explicitly,
while scratch run directories remain ignored.

## Reconcile again

From the repository root, use a fresh output directory:

```sh
nix-shell experiments/triage/shell.nix
experiments/triage/.venv/bin/python experiments/triage/review.py reconcile \
  --bundle experiments/triage/reviews/v1/bundle \
  --responses experiments/triage/reviews/v1/responses/1.json \
    experiments/triage/reviews/v1/responses/2.json \
    experiments/triage/reviews/v1/responses/3.json \
  --output experiments/triage/runs/review-v1-reconciled
```

The report and reviewed export reproduce byte-for-byte from the archived inputs.
Reconciliation rejects duplicate IDs across submissions and preserves per-record
reviewer identity. All 27 Python tests and the Go suite passed.

The next step is to settle the reply-versus-task contract and adjudicate context
flags and the two question cases. Preserve this first-pass evidence. Any revised
specification or scenarios require a new review; do not relax the gate to inflate
the accepted count or train on the selected four-class subset.
