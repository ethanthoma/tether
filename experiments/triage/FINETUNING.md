# Supervised MiniLM fine-tuning

The subsequent separate-set confidence experiment is in
[CALIBRATION.md](CALIBRATION.md). It leaves this model and its original policy unchanged.

This experiment trains the encoder as well as the classifier head. It uses only
the existing 261 synthetic training examples and 63 development examples. No new
data, model dependencies, inference service, or production changes are introduced.

## Fixed experiment

Start with the pinned public MiniLM encoder and the existing logistic-regression
head fitted to training examples. Preserve the previous/latest inbound/outbound
embedding slots, presence indicators, five labels, and saved JSON head format.
Fine-tune all encoder parameters and the head with weighted cross entropy:

- Seed 42, deterministic CPU operations, four threads, batches of 16, 12 epochs.
- AdamW: encoder learning rate 0.00002, head rate 0.001, weight decay 0.01.
- Inverse training-class-frequency loss weights; clip gradient norm at 1.
- Select the lowest unweighted development cross entropy, including the frozen
  epoch-zero checkpoint. Retain the original development cutoff grid and rule:
  maximize accepted coverage with zero accepted development errors.

The limits are fixed in `finetune.py`; this was not a hyperparameter search.
The CLI caps datasets at 2,000 training and 1,000 development cases. Long messages
fail rather than silently truncating. Best checkpoints use safetensors and JSON;
reload verification compares inference probabilities against training-time output.
Use a fresh output directory for every run.

## Result

Development selected epoch 12, reducing cross entropy from 1.1223 to 0.4136.
Raw development accepted accuracy increased from 24/41 to 43/48. However, some
errors were confident enough that no nonempty acceptance set passed the fixed
cutoff rule. The selected cutoff is **1.0**, leaving every example undecided.

On the same previously inspected 63-case synthetic regression set:

| Model | Accepted | Correct accepted | False reminders | False silencing | Actionable abstained |
| --- | ---: | ---: | ---: | ---: | ---: |
| Frozen encoder, no cutoff | 50 | 26 | 10 | 11 | 4 |
| Fine-tuned encoder, no cutoff | 48 | 46 | 2 | 0 | 2 |
| Fine-tuned encoder, selected cutoff | 0 | 0 | 0 | 0 | 29 |

The fine-tuned raw classifier also abstains correctly on all 13 ambiguous cases.
Its 46/48 accepted accuracy is a regression result on provisional synthetic labels,
not an independent production-accuracy estimate. The 63 cases represent only 12
correlated scenario families. No test result selected a checkpoint or changed a
threshold. The raw classifier remains unsuitable for autonomous reminders because
it still makes errors; the selected classifier provides no useful coverage.

Metrics, training history, artifact hashes, and predictions are recorded in
[finetune-report.json](finetune-report.json). Weights remain local in
`runs/minilm-finetuned-v1/`. Production continues to use its existing behavior.
An identical-seed rerun produced byte-identical encoder weights, JSON head, and
training history. All 12 Python tests, formatting checks, and the Go suite passed.

## Reproduce

Install the pinned environment from [TRAINING.md](TRAINING.md), and export the
synthetic splits as described in [SYNTHETIC.md](SYNTHETIC.md). From the repository root:

```sh
nix-shell experiments/triage/shell.nix
HF_HUB_OFFLINE=1 experiments/triage/.venv/bin/python experiments/triage/finetune.py \
  --data experiments/triage/runs/synthetic-v1/data.json \
  --output experiments/triage/runs/my-finetuned-model
HF_HUB_OFFLINE=1 experiments/triage/.venv/bin/python experiments/triage/train.py predict \
  --model experiments/triage/runs/my-finetuned-model \
  --data experiments/triage/runs/synthetic-v1/holdout.json \
  --output experiments/triage/runs/my-finetuned-model/predictions.json
experiments/triage/.venv/bin/python -m unittest discover \
  -s experiments/triage -p 'test_*.py' -v
```

The base encoder must already be cached for offline training. GPU and llama are
not needed. The existing prediction command loads the saved fine-tuned encoder
locally, so training does not require a new inference path.

## Evaluation boundary and next work

Keep the MDL diagnostic on the untouched public encoder. A fine-tuned encoder has
seen training labels, including those a prequential diagnostic would pretend to
withhold. `mdl.py` now pins the base model itself and no longer accepts `--encoder`.
Its previous 1.950 bits/label result reproduces unchanged. Measuring end-to-end
fine-tuning prequentially would require retraining from the public base for every
prefix; this experiment does not claim that measurement.

Next, independently review the synthetic labels and create new synthetic
calibration/evaluation families. Study confidence reliability without weakening
the acceptance rule against this regression set. A finer cutoff search alone is
not evidence of safety, and temperature scaling cannot change the predicted
class. Preserve this run as the encoder-training baseline.
