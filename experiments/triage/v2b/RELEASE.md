# V2b release plan

Frozen before fitting this candidate or observing its held-out test predictions.
The initial v2 candidate failed coverage on development data: threshold 1.0
accepted nothing. No predictions were generated on its test set. That test set
remains archived and is not reused for this release decision.

## Evidence and change

Development-only diagnosis found a context imbalance: only 50/893 training cases
had two messages, compared with 30/120 development cases. Two-message dev accuracy
was 5/30; single-message accuracy was 63/90. Pending requests followed by an
acknowledgment, promise, or partial answer were a major failure mode. Fourteen
regularization and text-feature baselines did not reach useful safe coverage.

Add independently blind-reviewed, fully synthetic training families covering
these distinctions and diverse everyday language. Retain the 893 approved v2
training cases; keep the 14 disputed families quarantined. Retain the existing
120-case development set for checkpoint and cutoff selection. Author and blind
review a fresh 120-case test set without access to old corpora or predictions.
Reject any family with a review disagreement or defect. Run exact-message,
family, and trigram separation checks before fitting. Record final counts and
input hashes in the training and release reports.

## Fixed recipe and acceptance

Keep the pinned MiniLM, seed 42, four CPU threads, 12 epochs, optimizer, weighted
loss, and batch size from v2 unchanged. Select the lowest development
cross-entropy checkpoint, including epoch zero; select the cutoff on the existing
fixed grid for maximum coverage with zero accepted development errors.

The fresh test must retain all five classes, at least 80 cases and 20 families.
Require zero errors among accepted predictions, at least 25% overall coverage,
and at least one accepted correct prediction for each actionable class. Explicit
abstentions and cutoff rejections do not count as coverage. Report raw accuracy,
class counts, coverage, errors, and limits of synthetic evidence. Do not tune on
test or relax the gate after seeing results. Failure leaves authority disabled.

## Production

Bend remains independently active. The CPU runtime can run read-only shadow
inference while classifier authority stays disabled. After passing the gate,
verify the exact immutable model/runtime under the production service account
without network access. Record its hash, latency, input rejection rate, and
service health. Activate only that hash; removing its switch restores fallback.
LLM outages leave undecided cases queued. No live mail enters the training data.
