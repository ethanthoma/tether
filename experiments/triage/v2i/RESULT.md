# V2i calibration result

Rejected before held-out inference. The frozen rule produced cutoffs of 0.975
for FYI, 0.90 for needs_reply, 0.80 for noise, and 0.975 for waiting_on_them.
Both development partitions had zero accepted errors and enough coverage:
42/152 (27.6%) on original development and 122/392 (31.1%) on fresh calibration.
Neither partition retained an accepted waiting_on_them case, failing the required
actionable-class support. The fresh 198-case test remains unused for predictions.

Artifact: `4da8777e321b82e43ef27db5df2775ea4c701801ee07642e83596d04b0a70e5e`.
The extra grid-step margin was an engineering hypothesis, not a release
requirement established by earlier evidence. V2j separately freezes a cutoff-only
trial without that extra step while preserving the actionable 0.90 floor and all
development/held-out acceptance gates. This rejection is retained unchanged.

Review retained 398/400 calibration cases; overlap quarantine removed six more.
All 200 test cases passed blind review, with two removed for overlap. These are
short synthetic examples with correlated paired families, not a measurement of
real-mail accuracy. Production classifier authority remains disabled.
