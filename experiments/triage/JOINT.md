# Joint-context CPU classifier

Current training and inference encode the complete latest one- or two-message
exchange together. Each message has an explicit speaker marker; `\n\n---\n\n`
separates messages. Attention can therefore compare a request with its response.
The normalized 384-dimensional MiniLM embedding is followed by four binary
position/direction indicators, producing 388 features for a five-class linear head.

The pinned 22.7M-parameter base supports 512 positions. Training sets and saves a
512-token combined-context limit, including markers and tokenizer special tokens.
Inference checks that setting, rejects oversized exchanges without truncation,
and preserves the Go adapter's 4,000-byte-per-body and 20-case request bounds.
Schema 3 requires `feature_layout: joint-thread-v1`, a 5×388 coefficient matrix,
and finite per-class `thresholds`. Archived layouts are rejected.

Class cutoffs use only development labels and the frozen grid in `train.py`.
Selection maximizes accepted cases with zero accepted development errors for each
predicted class. Unsupported classes use 1; 1 always abstains. Raw class probability
is not an accuracy guarantee. The release evaluator independently checks the
cutoffs against development data before making held-out predictions.

## Runs and evidence

- V2c's earlier layout accepted 46/120 held-out cases with four errors: rejected.
- V2d's joint encoder improved raw development accuracy to 101/120 and two-message
  accuracy to 25/30. Its safe cutoffs disabled waiting_on_them: rejected before test.
- [V2e](v2e/RELEASE.md) adds independently reviewed boundary examples using the same
  joint encoder and recipe. Its fresh test remains reserved until fitting ends.

Run the CPU fine-tuner with a fresh output directory:

```sh
nix-shell experiments/triage/shell.nix
HF_HUB_OFFLINE=1 experiments/triage/.venv/bin/python experiments/triage/finetune.py \
  --data experiments/triage/v2e/training.json \
  --output experiments/triage/runs/v2e-production/model
```

The data path is created only after blind review and partition checks. Training
records the exact data hash, recipe, checkpoint history, and cutoffs. The release
gate additionally requires zero accepted held-out errors, at least 25% coverage,
and both actionable classes. Synthetic evidence does not establish real-mail
accuracy. Production authority remains off until the gate, native CLI check,
and operational shadow verification pass; Bend's three switches are independent.
