# V2h calibrated checkpoint release plan

Frozen before fitting or held-out predictions. V2g failed its development
readiness requirement; the unchanged 154-case v2f test remains unused.

## Data

Append the independently blind-reviewed 200-case, 100-family direction supplement
to v2f training: 2,189 training and unchanged 152 development cases. Authors saw
the labeling policy, not evaluation cases or model predictions. All custom data
is fully synthetic. Preserve review packets and agreement records. Exclude whole
new families for exact normalized-message or trigram Jaccard overlap >=0.65 with
current or historical evaluation, without consulting labels or predictions.
Record input hashes, exclusions, and final separation before fitting.

## Fixed recipe

Keep the pinned NLI encoder, normalized mean-pooled joint-thread representation,
four direction bits, combined 512-token limit, seed 42, four CPU threads, 12
epochs, batch 16, logistic initialization, inverse-frequency weighted loss,
encoder learning rate 2e-5, head learning rate 1e-3, AdamW decay 0.01, and clip 1.

At every checkpoint including epoch zero, fit one positive temperature to actual
development logits. Use the existing bounded log-temperature optimizer over
[0.25, 8], at most 100 iterations and tolerance 1e-6, falling back to 1 when
unscaled NLL is no worse. Select the first minimum calibrated development NLL.
Record raw/calibrated NLL and temperature for every epoch. Preserve the selected
unscaled head; divide both saved weights and biases by temperature exactly once.
Bind the unscaled head hash in the inference artifact and training report.
Verify saved/reloaded probabilities and independently reproduce calibration
before held-out inference. Never rescale the live training head.

[Guo et al.](https://proceedings.mlr.press/v70/guo17a.html) support temperature
scaling as a calibration technique; they do not validate this checkpoint-selection
protocol. Development data selects temperatures, checkpoint, and cutoffs, so its
results are adaptive estimates. Positive scaling preserves each predicted class,
but need not preserve confidence ordering across examples or improve coverage.

## Unchanged gates and rollout

Keep the fixed classwise cutoff grid with zero accepted development errors.
Before test inference, require >=25% development coverage and correct accepted
support for both actionable classes. Evaluate the frozen candidate on v2f test
once: all five labels, >=80 cases, >=20 families, zero accepted errors, >=25%
coverage, and correct accepted support for both actionable classes. Failed test
consumes that holdout. Do not weaken gates or tune against test predictions.

Only an approved artifact proceeds through exact-wrapper CPU/resource checks,
isolated native activation/failure/rollback tests, live read-only shadow, and
hash-specific activation. Preserve all Bend switches, leave llama off, and send
no test notifications. Retain immutable model/runtime rollback and record hashes,
source revision, generation, and correlated synthetic-review limitations.
