# V2j held-out result

Passed the frozen synthetic quality gate. Both development partitions passed
with zero accepted errors: 43/152 (28.3%) on original development and 128/392
(32.7%) on the larger calibration set, including both actionable classes.

The fresh test was evaluated once: 64/198 accepted (32.3%), all correct, across
99 paired families. Accepted correct classifications were 22 needs_reply,
2 waiting_on_them, 7 FYI, and 33 noise. Raw accuracy was 174/198 (87.9%).
No ambiguous case was accepted. This does not establish real-mail accuracy:
examples are short, fully synthetic, correlated within families, and reviewed
by models rather than humans. Waiting_on_them coverage is especially limited.

Artifact: `4ac8bcd6afedea4f11fb44c5d23926f4ad1118eb740a4c7842d44b9f717ffab2`.
Cutoffs: FYI 0.975, needs_reply 0.90, noise 0.80, waiting_on_them 0.95;
explicit abstain remains 1. The encoder, head weights, and temperature are exactly
those from the pinned v2h source; only the recorded cutoff policy changed.

`calibration-report.json` and `release-report.json` contain hashes and complete
evidence. `native-cases.json` selects one already independently labeled example
per label from the approved evaluation for integration testing. These examples
test adapter behavior, not additional generalization.

Production activation additionally requires the exact-runtime native gate and
read-only live shadow. Deployment status and rollback belong in `PRODUCTION.md`.
