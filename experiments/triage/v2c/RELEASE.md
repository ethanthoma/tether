# V2c calibration release plan

Frozen before recalibration and before any held-out predictions. V2 and v2b both
failed their development coverage checks; neither generated test predictions.
V2b's fresh, independently reviewed 120-case test set remains unused and is the
single held-out evaluation for this candidate. Its labels did not inform this plan.

## Candidate change

Retain the exact v2b encoder and linear head weights, selected at epoch one by the
original fixed training recipe. No fitting or checkpoint change is permitted.
Replace the global cutoff with one cutoff for each predicted class, selected only
on the existing 120 development cases. Confidence is still a raw class probability,
not a calibrated accuracy guarantee.

Use the fixed grid 0, 0.40 through 0.95 in 0.05 increments, 0.975, 0.99, 0.995,
0.999, and 1. For each predicted class maximize accepted development coverage
subject to zero accepted development errors. A class with no safe accepted
examples is disabled at 1; explicit abstain also stays at 1. Threshold 1 always
rejects, even if floating-point confidence equals 1. Do not tune this grid on test.

Development-only diagnosis motivated this change: the worst v2b accepted-class
error scored 0.97426, above the old grid's highest usable cutoff. Exact global
selection could accept 22/120 cases; independent per-class boundaries could accept
29/120. These are development diagnostics, not test results or production approval.
The fixed extended grid may accept fewer. No architecture replacement is planned.

## Integrity and acceptance

Use a new artifact schema version and hash. Preserve old models and reports;
record source head, encoder, and training report hashes, development input hash,
fixed grid, selected cutoffs, and resulting development metrics. The active runtime
must accept only the new schema. The frozen training recipe and its original
checkpoint history remain subject to release validation.

Require the reviewed test to retain all five classes, at least 80 cases and 20
families. Run family, exact-message, and trigram separation before evaluation.
The unchanged production gate is zero errors among accepted test predictions,
at least 25% test coverage, and at least one accepted correct prediction for each
of needs_reply and waiting_on_them. Report raw accuracy and per-class results.
Do not change cutoffs or select a different candidate after inspecting this test.

If the gate fails, keep authority disabled and preserve the result. Any further
candidate requires new held-out families. Synthetic, correlated families cannot
establish real-mail reliability; production observation remains a separate check.

## Rollout

Only after passing, install the immutable CPU runtime and versioned model on atlas,
run the actual Go CLI against synthetic fixtures with activation/failure/rollback,
and inspect read-only live shadow coverage and latency. Activate the exact approved
hash. Keep rollback artifacts and all three existing Bend switches unchanged.
