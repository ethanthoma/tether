# Separate synthetic confidence calibration

This experiment freezes `minilm-finetuned-v1` and changes neither its weights nor
its original cutoff. It fits a separate, offline temperature/cutoff artifact.
Nothing is connected to production inference or reminders.

## Data and protocol

The three `calibration-*.json` files each contain 25 new, fully synthetic scenarios:
five per label and one example per family. No direction-reversed duplicates are
added. Every label has a visible evidence span. Labels remain provisional judgments
by the same author, without independent review. English, short-message, balanced
synthetic data does not establish performance on the actual mailbox distribution.

1. **Temperature:** fit one positive scalar to minimize negative log-likelihood,
   with temperature bounded to 0.25–8 and at most 100 optimizer iterations.
2. **Selection:** choose a cutoff using the unchanged grid and zero-accepted-error
   rule. Also choose an unscaled cutoff on this same partition as a control.
3. **Audit:** after writing and reloading the fixed policy, evaluate the untouched
   audit scenarios once. No audit result changes the temperature or cutoffs.

The model receives only messages and directions. Family IDs, labels, evidence,
and split names never become features. Exact normalized-message/family overlaps
and near-duplicate threads are rejected across partitions and against all earlier
seed, synthetic, and challenge data. The maximum cross-partition trigram Jaccard
similarity was 0.0909, below the pre-existing 0.65 rejection threshold. This lexical
check does not prove semantic independence.

[Guo et al., 2017](https://proceedings.mlr.press/v70/guo17a.html) motivates temperature
scaling as a simple post-processing calibration baseline. Dividing logits by a
positive temperature preserves each example's predicted class. It cannot repair
classification errors; whether confidence becomes useful must be measured.

## Results

Temperature **1.654538**; calibrated cutoff **0.80**; unscaled control cutoff **0.95**.
The calibrated policy accepts 16/25 selection examples, all correct. On the new audit:

| Policy | Accepted | Correct accepted | False reminders | False silencing | Actionable abstained |
| --- | ---: | ---: | ---: | ---: | ---: |
| No cutoff | 22 | 19 | 1 | 1 | 0 |
| Original cutoff 1.0 | 0 | 0 | 0 | 0 | 10 |
| Unscaled, newly selected cutoff 0.95 | 17 | 15 | 1 | 0 | 3 |
| Temperature-scaled, selected cutoff 0.80 | 18 | 16 | 1 | 0 | 3 |

The calibrated policy has 72% coverage and 16/18 accepted accuracy. Two ambiguous
examples are still assigned definite labels, including one actionable label. It
does **not** meet the desired error behavior, despite zero errors during selection.
The unscaled control shows that much of the coverage change comes from using the
new selection set, rather than temperature scaling alone.

Audit confidence metrics (all five labels, including the explicit abstain class):

| Metric | Before | After |
| --- | ---: | ---: |
| Negative log-likelihood | 0.6926 | 0.5303 |
| Multiclass Brier score, summed over classes | 0.2552 | 0.2495 |
| Expected calibration error, five fixed equal-width bins | 0.1339 | 0.0710 |

Lower is better for these metrics. ECE is especially sensitive to sample size and
binning. Calibration did not improve every split/metric: selection NLL worsened
from 0.3545 to 0.3618 and selection ECE from 0.0215 to 0.0808. These are small,
same-author synthetic samples, not calibrated population-risk guarantees.

The experiment also exposed a numerical bug in the shared prediction helper:
softmax probabilities can round to exactly one. Cutoff `1.0` now explicitly means
abstain on every case, even under saturation. A regression test covers this behavior.

## Reproduce

Use the pinned environment from [TRAINING.md](TRAINING.md), then run from the repository root:

```sh
nix-shell experiments/triage/shell.nix
HF_HUB_OFFLINE=1 experiments/triage/.venv/bin/python experiments/triage/calibrate.py \
  --model experiments/triage/runs/minilm-finetuned-v1 \
  --output experiments/triage/runs/my-calibration
experiments/triage/.venv/bin/python -m unittest discover \
  -s experiments/triage -p 'test_*.py' -v
```

Choose a fresh output directory. `calibration.json` records the fitted policy and
hashes of model weights, head, and fitting/selection data. `report.json` adds the
audit hash, confidence bins, decisions, and controls. The CLI uses only local
artifacts and the three checked-in partitions; no LLM, GPU, mailbox, or external API.
The checked-in [calibration-report.json](calibration-report.json) preserves this run.

Keep the policy offline. Next review the ambiguous-label specification and expand
synthetic training coverage using training-only diagnostics. Preserve this audit
as a regression set once inspected, and reserve new families for the next audit.
Do not loosen the acceptance rule to hide its failures here.
