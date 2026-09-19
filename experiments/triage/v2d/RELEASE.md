# V2d joint-context release plan

Frozen before fitting this candidate or observing its fresh held-out predictions.
V2c achieved 46/120 accepted test predictions but made four errors and was rejected.
That consumed test is archived and cannot approve this or any later candidate.
Its failure exposed a cross-message reasoning weakness; no test cases are added
to training, and the old test is not reused for selection.

## Representation and fixed recipe

Use the same 1,293 approved training cases and 120 development cases as v2b.
Encode each complete one- or two-message exchange as one text, with explicit
`Message from you:` or `Message from the other person:` markers and blank-line
message separators. This lets attention compare requests, responses, commitments,
and cancellations within the exchange. Append the four existing direction/position
presence bits to the normalized 384-dimensional embedding: 388 input features.

Use the same pinned MiniLM base and revision; verify its 512-position capacity.
Persist a 512-token combined-context limit, including speaker markers. Reject
oversized context instead of truncating it. The new artifact schema identifies
`joint-thread-v1`; previous layouts are not accepted by the new runtime.

Keep seed 42, four CPU threads, 12 epochs, batch size 16, logistic-regression head
initialization, weighted cross-entropy, AdamW settings, and gradient clipping
unchanged. Select the lowest development cross-entropy checkpoint, including epoch
zero. Select class-specific cutoffs using the fixed v2c grid and zero accepted
errors per predicted class. Unsupported classes and explicit abstain use cutoff
1, which always rejects. No choices may use the new test predictions.

## Evaluation

Author and independently blind-review a fresh 120-case, 30-family test without
access to earlier corpora or predictions. Quarantine any disputed family. Require
all five labels, at least 80 cases and 20 families. Verify family, exact-message,
and trigram separation before evaluating the frozen candidate once.

The unchanged production gate requires zero accepted test errors, at least 25%
coverage, and at least one accepted correct prediction in each actionable class.
Report raw accuracy, per-class results, counts, data/model/plan hashes, and complete
training history. Metadata integrity is not independent proof of chronology.
Synthetic correlated cases cannot establish real-mail reliability.

## Rollout

Keep Bend active and classifier authority disabled unless the gate passes. Then
package and copy the exact CPU runtime, install an immutable versioned model, and
run the actual Go CLI's isolated synthetic activation, failure, and rollback gate.
Inspect live read-only shadow latency, input rejections, and service health before
activating only the approved artifact hash. Retain the prior system and model for
rollback; no training on private mail and no test notifications.
