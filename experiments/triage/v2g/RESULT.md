# V2g development result

Rejected before held-out evaluation: the preregistered development requirement
was 25% safe coverage; the selected epoch accepted 29/152 (19.1%), all correct.
Both actionable classes retained support. Raw accuracy was 92/152 (60.5%).
The 154-case v2f test remains unused for predictions.

The complete 12-epoch run selected epoch one by minimum raw development
cross-entropy. Later checkpoints improved raw accuracy while cross-entropy
worsened, consistent with increasingly confident errors. This motivates a
separate calibration experiment; it does not establish that calibration will
meet coverage or safety requirements.

A development-only frozen-encoder probe also compared CLS and the original
NLI pooler. They achieved 68/152 and 58/152 raw accuracy, with safe coverage
1/152 and 0/152 respectively. Mean pooling remains the chosen representation.
See `pooling-probe.json`; no held-out inference was involved.

The exact production runtime passed a synthetic maximum-length resource check
on Atlas (`atlas-preflight.json`). That establishes operational feasibility,
not classifier quality. Classifier authority remains disabled; Bend remains
active in production.
