# Joint-context CPU classifier

Current training and inference encode the complete latest one- or two-message
exchange together. Each message has an explicit speaker marker; `\n\n---\n\n`
separates messages. Attention can therefore compare a request with its response.
The normalized 384-dimensional encoder embedding is followed by four binary
position/direction indicators, producing 388 features for a five-class linear head.

The current [v2g base](v2g/RELEASE.md) is a pinned DeBERTa language-inference
encoder with explicit mean pooling; its original NLI output head is discarded.
It has approximately 70.7M encoder parameters and supports 512 positions.
Training sets and saves a
512-token combined-context limit, including markers and tokenizer special tokens.
Inference checks that setting, rejects oversized exchanges without truncation,
and preserves the Go adapter's 4,000-byte-per-body and 20-case request bounds.
Schema 3 requires `feature_layout: joint-thread-v1`, a 5×388 coefficient matrix,
and finite per-class `thresholds`. Archived layouts are rejected.

Class cutoffs use only development labels and the frozen grid in `train.py`.
The current fine-tuner fits a bounded scalar temperature at each checkpoint and
selects the first minimum calibrated development cross-entropy. It preserves the
unscaled head, folds temperature into both weights and biases, and binds that
source head's hash in the inference artifact. Release checks independently
reproduce the calibration before held-out inference. Development estimates are
adaptive because checkpoint, temperature, and cutoffs share that partition.
Selection maximizes accepted cases with zero accepted development errors for each
predicted class. Unsupported classes use 1; 1 always abstains. Raw class probability
is not an accuracy guarantee. The release evaluator independently checks the
cutoffs against development data before making held-out predictions.

## Runs and evidence

- V2c's earlier layout accepted 46/120 held-out cases with four errors: rejected.
- V2d's joint encoder improved raw development accuracy to 101/120 and two-message
  accuracy to 25/30. Its safe cutoffs disabled waiting_on_them: rejected before test.
- [V2e](v2e/RESULT.md) accepted 37/120 held-out cases with two false reminders:
  rejected. Its test is consumed.
- [V2f](v2f/RESULT.md) accepted only 26/152 development cases and was rejected
  before test. Its 154-case test remains unused.
- [V2g](v2g/RESULT.md) accepted 29/152 development cases (19.1%): rejected
  before test despite passing the synthetic Atlas resource check.
- [V2h](v2h/RELEASE.md) adds 200 reviewed direction examples and calibrated
  checkpoint selection. Its unchanged held-out gate remains mandatory.

Run the CPU fine-tuner with a fresh output directory:

```sh
nix-shell experiments/triage/shell.nix
HF_HUB_OFFLINE=1 experiments/triage/.venv/bin/python experiments/triage/finetune.py \
  --data experiments/triage/v2h/training.json \
  --output experiments/triage/runs/v2h-production/model
```

For the first run, cache the pinned base inside the same Nix shell:

```sh
PYTHONPATH=experiments/triage experiments/triage/.venv/bin/python - <<'PY'
from huggingface_hub import snapshot_download
from train import BASE, REVISION

snapshot_download(BASE, revision=REVISION, allow_patterns=[
    "config.json", "model.safetensors", "tokenizer_config.json", "tokenizer.json",
    "special_tokens_map.json", "added_tokens.json",
])
PY
```

`load_base_encoder` loads only that cached revision with remote code disabled.
The data path is created only after blind review and partition checks. Training
records the exact data hash, recipe, checkpoint history, and cutoffs. The release
gate first requires 25% development coverage and both actionable classes before
held-out inference. It additionally requires zero accepted held-out errors, at least 25% coverage,
and both actionable classes. Synthetic evidence does not establish real-mail
accuracy. Production authority remains off until the gate, native CLI check,
and operational shadow verification pass; Bend's three switches are independent.
