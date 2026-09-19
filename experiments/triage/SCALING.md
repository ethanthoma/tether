# Synthetic data scaling

The original training set had 261 cases from 48 scenario families. The expansion
in `scale_scenarios.json` adds 100 training families, 20 development families, and
20 fresh test families. Combining the original train/dev splits with these new
families produces 981 training cases, 207 development cases, and 144 test cases.
Training now covers 148 families; direction reversals are correlated augmentations,
not additional independent situations. The number of training noise cases grows
from six to 46.

The new cases cover more domains, informal writing, partial completion,
cancellations, deadlines, mixed ownership, and bulk versus personal mail. All
content is synthetic. Labels remain single-author self-reviewed, not independently
validated. Most messages are short and their obligation cues explicit.

## Comparison protocol

`scale.py` audits family boundaries, exact message overlap, and cross-partition
three-word shingle similarity before exporting data. New cases are checked against
all earlier synthetic scenarios, seed/challenge cases, and calibration partitions.
Earlier test and calibration cases never become training examples.

The three training sets contain the original 48 families plus zero, 50, or 100
new families. New families are shuffled once with seed 42, then added whole; the
subsets are nested. All runs use the same 207 development cases, pinned MiniLM
base, seed, and 12-epoch recipe from `finetune.py`. The small model is retrained
because its earlier checkpoint was selected using a different development set.
Larger datasets entail more optimizer updates at fixed epochs: this measures the
recipe's response to more data, not a compute-matched causal effect of data alone.

`evaluate_scale.py` writes the lowest-development-loss candidate to `selection.json`
before loading test cases. It reports all three learning-curve points without
selecting or tuning on test results. Confidence cutoffs retain the existing rule:
maximum development coverage with zero accepted errors on the fixed grid.

## Reproduce

Use the pinned environment from [TRAINING.md](TRAINING.md), with the base model
already cached. From the repository root, enter the Nix shell and run:

```sh
nix-shell experiments/triage/shell.nix
experiments/triage/.venv/bin/python experiments/triage/scale.py \
  --output experiments/triage/runs/scaling-reproduction
for size in small medium full; do
  HF_HUB_OFFLINE=1 experiments/triage/.venv/bin/python experiments/triage/finetune.py \
    --data experiments/triage/runs/scaling-reproduction/$size.json \
    --output experiments/triage/runs/scaling-reproduction/$size-model
done
HF_HUB_OFFLINE=1 experiments/triage/.venv/bin/python experiments/triage/evaluate_scale.py \
  --run experiments/triage/runs/scaling-reproduction
HF_HUB_OFFLINE=1 experiments/triage/.venv/bin/python experiments/triage/mdl.py \
  --data experiments/triage/runs/scaling-reproduction/full.json \
  --output experiments/triage/runs/scaling-reproduction/mdl.json
```

Everything runs on CPU. Model weights and generated run directories remain ignored.
The MDL diagnostic uses the untouched public encoder, never a fine-tuned checkpoint.
