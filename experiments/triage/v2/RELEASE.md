# V2 production release gate

This plan is frozen before fitting the v2 model or observing its test predictions.
Training uses only the 893 approved v2 training cases. A fresh, independently
blind-reviewed evaluation corpus supplies development and test families. Any
review disagreement or defect quarantines the entire family before training.
No old v1 evaluation labels or quarantined training families are included.

## Selection and acceptance

Use the existing deterministic CPU recipe: MiniLM at the pinned revision,
seed 42, 12 epochs, four threads, and the existing optimizer settings. Select
checkpoint by development cross-entropy, including epoch zero. Select the
confidence cutoff using only development data: maximum accepted coverage with
zero accepted errors on the existing threshold grid. Never tune on test results.

The frozen test must retain all five labels and at least 80 cases across 20
families. Production acceptance requires zero errors among accepted predictions,
at least 25% overall test coverage, and at least one correct accepted prediction
for each actionable class (`needs_reply` and `waiting_on_them`). Explicit abstain
predictions and confidence rejections are excluded from accepted coverage.
Report raw accuracy, coverage, accepted errors, per-class counts, and uncertainty;
synthetic tests do not establish real-mail accuracy.

A failed gate leaves the candidate disabled. Further experiments require a new
recorded plan and fresh held-out families; do not weaken this gate after results.

## Activation and rollback

Verify the exact immutable CPU runtime and model on atlas without network access.
Activate only the approved artifact SHA-256 in `triage-model.enabled`. The Go
adapter accepts only policy `reply-triage-v2` and that exact hash. Rejected inputs,
abstentions, timeouts, and model failures use the existing LLM path; when that
service is unavailable, undecided threads stay queued without consuming retries.

Removing the switch restores the existing path on the next invocation. Retain
the previous NixOS generation and deployment source. Do not train on live mail
or send test notifications. Review aggregate inference and normal service records
following deployment; keep synthetic evaluation separate from live observations.
