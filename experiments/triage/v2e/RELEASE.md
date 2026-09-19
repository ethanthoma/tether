# V2e contextual-boundary release plan

Frozen before fitting this candidate or observing its held-out predictions.
The joint-context v2d run improved raw development accuracy to 101/120 and
accepted 26 correct cases, but disabled waiting_on_them after a confident mixed-
ownership error. It cannot meet the actionable-class gate and was not evaluated
on test. Its fresh, independently reviewed 120-case test remains untouched and
is reserved for this candidate's single held-out evaluation.

## Data and recipe

Retain the 1,293 approved v2b training cases and 120 development cases. Add up to
400 independently blind-reviewed training cases covering mixed ownership,
clarification questions, partial versus complete answers, cancellations, promises,
non-prose tasks, and automated receipt/deadline boundaries. Aim for at least 240
new two-message cases and meaningful representation of all five labels. No old
test examples enter training. Quarantine entire disputed families and verify
family, exact-message, and trigram separation before fitting.

Keep the v2d joint-thread representation: speaker markers, a blank-line/dash
separator (`\n\n---\n\n`), one normalized 384-dimensional joint embedding, and four
position/direction presence bits. Use schema 3, `joint-thread-v1`, and a combined
512-token limit including markers; reject oversized contexts without truncation.

Keep the pinned MiniLM, seed 42, four CPU threads, 12 epochs, batch size 16,
logistic head initialization, weighted loss, optimizer settings, and gradient
clipping unchanged. Select the lowest development cross-entropy checkpoint,
including epoch zero. Select per-predicted-class cutoffs on the existing fixed
v2c grid with zero accepted development errors. Unsupported classes and explicit
abstain remain disabled at cutoff 1.

## Acceptance and rollout

Use the frozen v2d test only after model and cutoffs are fixed. Require all five
classes, at least 80 cases and 20 families, zero accepted errors, at least 25%
coverage, and at least one accepted correct prediction in each actionable class.
Record all source, model, and plan hashes, complete training history, per-class
metrics, and limitations of synthetic correlated samples. Do not alter the gate
or cutoffs after reading test results. A failed test requires a new holdout for
any later candidate and leaves authority disabled.

After passing, package the exact CPU runtime and immutable model, run the native
Go CLI's isolated activation/failure/rollback gate, and inspect live read-only
latency and input rejection rates. Activate only the approved hash and retain
rollback artifacts. Keep all three Bend switches active and llama off. No private
mail enters training and no test notifications are sent.
