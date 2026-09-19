# Synthetic data scaling

The subsequent [blind review workflow](REVIEW.md) prepares this training corpus
for a separate reviewer under the explicit [labeling specification](LABELING.md).

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

## Results

All results below use the same fresh 144-case, 20-family synthetic test set.
Raw accuracy counts both correct classifications and correct abstentions.

| Training cases / families | Selected epoch | Dev loss | Raw accuracy | Cutoff | Accepted / correct | False reminders |
| --- | --- | --- | --- | --- | --- | --- |
| 261 / 48 | 11 | 0.37455 | 125/144 (86.8%) | 1.00 | 0 / 0 | 0 |
| 609 / 98 | 11 | 0.04212 | 140/144 (97.2%) | 0.75 | 109 / 109 | 0 |
| 981 / 148 | 10 | 0.02270 | 141/144 (97.9%) | 0.65 | 110 / 108 | 2 |

The last two columns apply the development-selected confidence cutoff. The full
model was selected by development loss before test evaluation. Its two accepted
errors are direction reversals of one canceled-request family: a message saying
the campsite supplied a tent and to ignore the previous borrowing request. It
also abstained on two actionable cases; no accepted prediction falsely silenced
an actionable case. The medium model abstained on one actionable case.

Additional families substantially improved raw classification, with smaller gains
between 609 and 981 rows. Selective correctness did not improve monotonically.
Do not switch candidates or retune cutoffs using this test: it is now a regression
set. The zero observed accepted errors for the medium model are not a production
error-rate guarantee, especially with only 20 correlated, same-author test families
and a single training seed. Neither model is deployed.

The expanded corpus's conditional prequential label code is **1.631 bits/case**,
versus **2.194** for its class-frequency prior, **2.322** for uniform labels, and
**2.554** with shuffled labels. These are three-order-seed means using the untouched
base encoder. The earlier corpus scored 1.950 bits/case, but the corpora have
different composition: the decrease demonstrates more compressible label structure
under this diagnostic, not independently better labels or real-mail generalization.

[scaling-report.json](scaling-report.json) records split manifests, training history,
artifact hashes, the frozen selection, and every prediction.
[scaling-mdl-report.json](scaling-mdl-report.json) records the coding diagnostic.
All 18 Python tests and the Go suite passed; changed Python files passed Ruff.

The next expansion should vary writing style and conversation structure more
aggressively, especially implicit cancellations and context-dependent obligations.
Independent synthetic label review and a fresh evaluation set matter more than
inflating the count with additional direction flips. Keep the current test fixed.

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
