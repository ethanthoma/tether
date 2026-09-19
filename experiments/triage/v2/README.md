# V2 synthetic training corpus

This is a semantic relabeling of the existing 981 training cases from 148 families
under [reply-triage-v2](../LABELING.md), not 981 newly generated situations.
Message text, direction, and family membership are unchanged. Development and test
cases were not included. The v1 corpus, models, and review remain immutable.

## Authoring

Three shards were assigned by whole family. Each author received only opaque IDs,
messages, and the v2 specification in a fresh model context, without old labels
or model predictions. Authors personally labeled every case and supplied an exact
evidence span plus a case-specific reason. Scripts joined these recorded decisions;
they did not infer labels from keywords, old flags, obligation tags, or reversal.

The usage-limit interruption left two complete shard files and 255 saved
annotations for the first shard. A fresh recovery context inspected all 332 cases
in that shard, preserved the saved decisions, and completed the remainder.
The authoring archive records the final submissions, inputs, and source mapping.

One clarification was shared during authoring: a visible explicit acknowledgment
of the user's request can establish that request without a separate outbound
message. An unsolicited promise alone cannot. This applies the visible-request
rule without requiring invented history.

| Label | V1 | V2 candidate |
| --- | ---: | ---: |
| abstain | 214 | 447 |
| fyi | 259 | 254 |
| needs_reply | 245 | 100 |
| noise | 46 | 51 |
| waiting_on_them | 217 | 129 |

There are **250 changed labels**. The larger abstain class includes tasks and
commitments that the reply classifier cannot safely route, not just uncertain
messages. This is intentional under v2; it does not demonstrate that a trained
model will detect those boundaries correctly.

## Traceability and rebuilding

`authoring/` contains each packet and final manual annotation file, plus a
manifest mapping opaque IDs back to the frozen v1 training snapshot. The semantic
author submissions are inputs to `relabel.py`; the script validates complete
coverage, policy identifiers, original messages, labels, evidence, and source
hashes before creating output.

`corpus/candidate.json` records v2 labels and per-case author, evidence, rationale,
previous label, and unresolved author flags. Old sender/recipient state annotations
are removed because their mechanical label mapping is invalid under v2.
`corpus/audit.json` enumerates every label change and hashes the author inputs.
The candidate remains provisional until the separate blind review gate.

From the repository root:

```sh
nix-shell experiments/triage/shell.nix
experiments/triage/.venv/bin/python experiments/triage/relabel.py \
  --source experiments/triage/reviews/v1/bundle/author/source.json \
  --authoring experiments/triage/v2/authoring \
  --output experiments/triage/runs/my-v2-candidate
```

Use a fresh output directory. Rebuilding the recorded candidate and audit produced
byte-identical files. The candidate's root policy metadata is enforced by review
preparation. A declaration alone is not evidence of semantic relabeling.

## Review boundary

The subsequent review uses three different fresh contexts, 327 shuffled cases each.
Reviewers receive no author labels, reasons, source IDs, family metadata, or prior
review findings. This is one separate model-context judgment per case, not human
validation or a multi-model consensus. Task-level file restrictions do not provide
an OS sandbox.

During review, the visible-request clarification was repeated and the literal prose
boundary clarified: requesting a photo/file transfer alone does not request prose;
asking to state or confirm information does. Explicit quoted history can establish
a request if participant roles are clear. Unresolved interpretations should be
flagged, not silently turned into policy exceptions.

The existing whole-family gate withholds any family with missing judgments,
label disagreements, reviewer flags, or author flags. Author concerns cannot be
cleared merely by reviewer agreement. V1's archived reconciliation still
reproduces byte-for-byte.

## Adoption

The [completed blind review](../reviews/v2/README.md) agrees on 967 labels and
withholds 14 whole families. Its export contains **893 cases from 134 families**,
including all five labels. It preserves 412 valid abstentions; these are no longer
blanket-flagged as defects. The candidate and audit above remain the frozen inputs
to that review, not a silently rewritten approved corpus.

Fresh, separately blind-reviewed development and test families now accompany
this corpus. The first v2 training run failed the frozen release coverage gate;
its safe cutoff abstains on every case. See [production progress](PRODUCTION.md)
and the complete `training-report.json`. The old v1 evaluation labels remain
unused for v2, disputed families remain quarantined, and test predictions have
not been used for model selection.
