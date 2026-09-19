# V2 blind training-label review

All 981 v2 candidate training cases were reviewed once in three fresh model
contexts, with 327 shuffled cases per reviewer. Reviewers saw only opaque IDs,
messages, the v2 specification, and the interpretation clarifications documented
in the [authoring record](../../v2/README.md). They did not receive candidate labels,
old labels, author rationales, family names, or model predictions.

These were separate contexts with inherited model settings, not human review
or established model diversity. File restrictions were instructions, not an OS
sandbox. All reviewers attested that author labels remained unseen.

## Result

| Measure | Count |
| --- | ---: |
| Reviewed cases / families | 981 / 148 |
| Author/reviewer label agreement | 967 |
| Label disagreements | 14 |
| Reviewer flags | 1 label ambiguity |
| Withheld whole families / cases | 14 / 88 |
| Exported whole families / cases | 134 / 893 |

The ambiguity flag overlaps a label disagreement; it is not a fifteenth disputed
case. There were no unresolved author flags. Every case has a judgment, exact
evidence, and rationale. No disagreement was automatically resolved by replacing
the author's label or dropping an inconvenient variant.

The reviewed export covers all five labels:

| Label | Cases |
| --- | ---: |
| abstain | 412 |
| fyi | 233 |
| needs_reply | 89 |
| noise | 45 |
| waiting_on_them | 114 |

This fixes the missing-abstain coverage in the v1 filtered export. It does not
establish accuracy on real mail or certify every agreed label as correct.

## Remaining disagreements

The 14 cases span these boundaries:

- A prose answer versus a document transfer or another concrete action.
- An answered question versus a separate unfinished user commitment.
- A visible request acknowledgment versus an unsolicited promise.
- Routine automated noise versus personalized informational updates.
- A status question versus a request to return an item. The costume-return
  example is explicitly flagged as ambiguous.

See `summary.json` for exact case IDs and `reconciled/report.json` for both
labels and the reviewer's evidence and rationale. Author reasons and old-label
provenance remain in the source snapshot. The entire affected family stays out
of the export pending adjudication; no v1 labels or judgments changed.

## Audit and reproduction

- `bundle/reviewer/packet.json`: full blinded input and specification snapshot.
- `bundle/author/`: complete candidate source, mapping, and integrity hashes;
  these were not reviewer inputs.
- `responses/{1,2,3}.json`: original judgments. The packet order defines shard
  offsets 0–326, 327–653, and 654–980.
- `reconciled/report.json`: per-case comparison and family gate decisions.
- `reconciled/reviewed.json`: the 893-case, training-only export.
- `summary.json`: counts, withheld families, class coverage, and artifact hashes.

Each response was checked against its exact assigned IDs, not just the full
packet's ID set. Reconciliation rejects overlaps across submissions. To reproduce
the report and export from the repository root, choose a fresh output directory:

```sh
nix-shell experiments/triage/shell.nix
experiments/triage/.venv/bin/python experiments/triage/review.py reconcile \
  --bundle experiments/triage/reviews/v2/bundle \
  --responses experiments/triage/reviews/v2/responses/1.json \
    experiments/triage/reviews/v2/responses/2.json \
    experiments/triage/reviews/v2/responses/3.json \
  --output experiments/triage/runs/my-v2-review
```

The report and export reproduced byte-for-byte. All 38 Python tests and the Go
suite passed; changed Python passed Ruff. The unchanged v1 audit also reproduces.

## Training boundary

No v2 model has been trained or deployed. The old development, calibration, and
test labels remain v1 and cannot be mixed into a v2 model evaluation as-is.
Prepare separately reviewed v2 development data and fresh evaluation families
before selecting or promoting a new checkpoint. Keep these 14 disputed training
families quarantined while resolving their policy interpretations.
