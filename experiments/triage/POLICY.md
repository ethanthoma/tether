# Production policy alignment

## Decision and boundary

[Labeling version 2](LABELING.md) adopts the reply semantics in
[`triageSystemPrompt`](../../triage.go). Human-requested prose replies and explicit
automated personal deadlines can require a reply; waiting requires a visible user
request. This document does not change production's prompt or thread states.

The five-label classifier cannot extract commitments or represent every task.
An outstanding user task, user promise, or unsolicited incoming promise must not
silently become informational mail. Such cases abstain unless the visible exchange
supports a permitted label without losing a separate routing obligation. The
existing triage/commitment path remains necessary. Calendar notifications retain
production's explicit informational exception.

Version 2 is an authoring and review contract, not a claim that the current model
implements it. Existing training reports measure version 1. Any shadow comparison
must identify that policy mismatch; agreement with old labels does not establish
production suitability. Shadow predictions must not update threads, create
commitments, or send reminders.

## Why the first review cannot simply be unblocked

The immutable [v1 audit](reviews/v1/README.md) records 231 policy flags and 104
context flags. Counts reflect its broader action-label policy and reviewer
interpretations, not automatically defective examples. Task-only cases need new
labels; intentional uncertainty can remain a valid, unflagged abstention. Removing
flags from old judgments would erase which specification the reviewers saw.

The questions in `locker_location_2` and `ceramics_booking_2` visibly ask for an
answer despite missing historical referents. Under version 2 the inbound versions
require a reply and the outbound versions establish waiting. This is a documented
proposed adjudication, not an edit to their labels or archived reviewer responses.
Missing information needed to *answer* differs from missing information needed to
identify *who should answer*. Rhetorical, quoted, or canceled questions still need
their surrounding context considered.

Direction reversal also changes the policy boundary: a task requested by the user
can establish waiting, while the same task requested of the user cannot establish
a prose-reply duty. The old sender/recipient obligation mapping is insufficient
for version 2; it must not generate new labels unchanged.

## Relabel and review protocol

The [first v2 training pass](v2/README.md) now retains 893 blind-reviewed cases
from 134 families. Development/evaluation preparation and model training remain
pending. The protocol below records the requirements for this and future passes.

1. Preserve all v1 corpora, review packets, judgments, reports, and model artifacts.
   Create a separately versioned candidate corpus. Review preparation requires
   root metadata `"label_policy": "reply-triage-v2"`, preserved in the packet,
   source snapshot, report, and reviewed export. This declaration is necessary
   but cannot substitute for semantic relabeling; old corpora are rejected as-is.
2. Relabel complete training families from their visible messages under version 2;
   retain evidence and a per-case reason for every change. Inspect every family,
   not just previously flagged cases. Do not mass-convert labels from keywords,
   state tags, old flags, or direction alone.
3. Review the proposed question adjudications and institutional-notice reversals.
   Repair implausible scenarios as new revisions with provenance; do not discard
   inconvenient variants to make a family pass.
4. Create fresh blind packets with the version 2 specification. Collect new
   judgments, resolve disagreements, and retain the whole-family export gate.
   Confirm all five labels remain represented before training.
5. Keep development and test families outside that training review. Separately
   revise/review their policy labels and reserve fresh evaluation families before
   tuning. Rerun family, overlap, and split checks before retraining/calibration.

Production adoption additionally needs evidence of useful coverage, false-reminder
and false-silencing rates, commitment-path preservation, and measured shadow
latency/failures. Neither a larger corpus nor model-review agreement establishes
those conditions on its own.
