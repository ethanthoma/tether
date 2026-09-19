# V2i frozen-model cutoff release plan

Frozen before new calibration inference or held-out predictions. V2h improved
coverage but failed with four accepted errors. Its test is consumed and will not
be used for this trial's fitting or evaluation.

## Fixed model and fresh data

Freeze source artifact
`951f1830d3c52e5f899d080776fecd00b5b66ff39c3ca6cb85c8e3731fe00bac`:
the exact v2h encoder, head coefficients, biases, temperature, tokenizer, and
512-token joint-thread representation. No optimization, checkpoint selection,
temperature refitting, or new feature design is permitted. Preserve and verify
the source training/calibration provenance against `v2h/training.json`.

Independently author and blind-review 400 new calibration cases (200 families)
and 200 new test cases (100 families), balanced across the five labels before
quarantine. Authors see only the labeling policy, not prior corpora or predictions.
Reviewers see only blinded packets. Whole families with disagreements or defect
flags are withheld. Exclude whole new families for exact normalized-message or
trigram Jaccard overlap >=0.65 with prior training/evaluation or the other new
partition, without consulting labels or predictions. Preserve the audit and
require at least 300 calibration cases and 150 families after exclusions.
Record all input hashes before inference. All new custom data is synthetic.

## Conservative cutoff rule

Use the existing fixed grid and zero-error classwise selection on the new
calibration partition. For each class, start with the maximum of its source
cutoff and its new calibration cutoff. For `needs_reply` and `waiting_on_them`,
advance that maximum by one grid position, then impose a minimum of 0.90.
The value 1 remains 1 and always abstains. Other classes receive no extra margin;
the explicit abstain class remains 1. No source cutoff can be lowered.

This margin is a preregistered engineering choice, not a statistical guarantee.
It trades some of v2h's coverage for safety. Do not adjust it after any new
predictions. Save a distinct artifact with source identity, calibration-data
identity, exact rule, and cutoff evidence. Independently verify unchanged encoder
files and head parameters and reproduce the rule before test inference.

## Gates and deployment

The original development partition and the new calibration partition must each
have >=25% accepted coverage, zero accepted errors, and correct accepted support
for both actionable classes before test inference. Evaluate the fresh holdout
once, requiring all five labels, >=80 cases, >=20 families, zero accepted errors,
>=25% coverage, and both actionable classes supported. Reserve the report before
inference; an interrupted reservation means the test may be consumed.

Only a passing artifact proceeds to exact-runtime resource checks, isolated
native activation/fallback/rollback, live read-only shadow, then hash-specific
production activation. Keep rollback, all three Bend switches, and llama off;
send no test notifications. Record correlated synthetic-review limitations and
do not claim measured real-mail accuracy.
