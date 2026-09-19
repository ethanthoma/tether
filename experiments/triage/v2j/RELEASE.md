# V2j supported cutoff release plan

Frozen before fitting this candidate or consuming the still-unused v2i holdout.
V2i's extra grid-step margin preserved safe coverage but eliminated every accepted
waiting_on_them prediction. That candidate remains rejected.

## Fixed inputs and intervention

Keep source model
`951f1830d3c52e5f899d080776fecd00b5b66ff39c3ca6cb85c8e3731fe00bac`,
all encoder/head weights, temperature, tokenization, and source training
provenance unchanged. Use the same original 152 development cases and the
reviewed, overlap-audited 392 cases in `v2i/calibration.json`. Reserve unchanged
`v2i/test.json` (198 cases, 99 families) for one evaluation.

Apply the existing zero-error classwise grid selector to the fresh calibration
partition. Take the maximum of each source and calibration cutoff. For each
actionable class impose a minimum of 0.90, without the additional grid step used
by v2i. Abstain remains 1; 1 always abstains. No source cutoff can be lowered.
No further cutoff change, model fitting, or temperature fitting belongs to this
trial. Record a distinct artifact, input identities, rule, and report.

This is adaptive development: calibration results motivated the change, so they
are not independent evidence of generalization. The fresh test has not been used
for predictions or tuning. No statistical safety guarantee is inferred from
zero development errors.

## Unchanged acceptance and rollout

Both original development and calibration must separately have >=25% coverage,
zero accepted errors, and accepted correct support for both actionable classes.
Verify the pinned source, original training-data/report hashes, all unchanged
encoder files and head parameters, and independently reproduce cutoffs before
held-out inference. The held-out gate remains all five labels, >=80 cases,
>=20 families, zero accepted errors, >=25% coverage, and both actionable classes
supported. Reserve the output exclusively before inference. A failed held-out
run consumes the test; do not tune this candidate afterward.

An approved artifact still requires exact-runtime resource and native integration
checks, read-only live shadow, and hash-specific activation with rollback. Preserve
all Bend switches, keep llama off, and send no test notifications. Report short,
correlated synthetic-data limitations rather than claiming real-mail accuracy.
