# V2f diverse-conversation release plan

Frozen before fitting this candidate or observing its held-out predictions.
V2e accepted 37/120 test cases with two false reminders, so it is rejected.
That test is consumed and cannot approve another candidate.

## Data intervention

Retain the 1,689 approved v2e training cases. Add up to 300 independently
worded cases from 150 new families, with approximately balanced labels and at
least 230 two-message cases before review. Emphasize requests followed by
responses, including partial completion, clarification, acknowledgment, and
remaining obligations. At least 40 new waiting examples should end inbound.
Authors must not generate domain substitutions through shared prose templates;
check repeated sentences across new families and retain the audit.

Retire the repeatedly used development set from selection for this candidate.
Independent author contexts create fresh 160-case development and test sets,
each with 80 families, all five labels, and at least 100 two-message cases before
review. Authors see only the labeling policy, not previous corpora or model
predictions. Separate blind contexts review all three additions. Quarantine
entire families with disagreements or defect flags; do not relabel to fit a
model. Preserve every source and review. Check family, exact-message, and
trigram separation before fitting. Report final counts after quarantine.

Before fitting, an overlap audit found independently authored messages shared
across partitions. Preserve reviewed sources unchanged and derive separate
exports: quarantine every new family participating in an exact normalized
message collision or cross-partition trigram Jaccard similarity of at least
0.65. Preserve existing training families; for collisions between new
development and test families, exclude both families. Apply this rule without
using labels or predictions and record all excluded family IDs. Evaluate only
the resulting frozen `test.json`, after separation passes.
Apply the same exclusion to new families overlapping historical evaluation
data: the original cases/seed development sets, generated synthetic and scale
development/test sets, three calibration sets, and v2/v2b/v2d evaluations.
Record reference-file hashes and verify each reviewed case's declared split
before assembly. Historical sources and existing training remain unchanged.

## Fixed recipe

Keep schema 3, joint-thread-v1, the pinned MiniLM revision, combined 512-token
limit, seed 42, four CPU threads, 12 epochs, batch size 16, weighted loss,
logistic head initialization, optimizer settings, and gradient clipping from
v2e. Select the first minimum fresh-development cross-entropy checkpoint,
including epoch zero. Select per-class cutoffs using the unchanged fixed grid
and zero accepted development errors. Unsupported classes abstain.

Development remains a selection set, not unbiased accuracy evidence. The test
is evaluated once only after weights and cutoffs are fixed. No old test or
development case is added to training. Do not relax cutoffs after test results.

## Acceptance and production

Keep the gate: all five test labels, at least 80 cases and 20 families, zero
accepted errors, at least 25% overall coverage, and a correct accepted case in
each actionable class. Record artifact, data, plan, and report hashes; disclose
synthetic-family correlation and model-context review limitations.

Only a passing model proceeds to the isolated native CLI activation/failure/
rollback check and read-only production verification. Activate its exact hash
after those pass. Preserve rollback artifacts, all three Bend switches, and
the offline llama service. Do not send test notifications or use private mail
as training data. A failed test consumes this holdout and leaves authority off.
