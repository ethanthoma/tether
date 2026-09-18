# Local triage classifier pilot

The current fully synthetic dataset and MDL diagnostic are documented in
[SYNTHETIC.md](SYNTHETIC.md). This page preserves the original pilot for comparison.

This experiment trains a custom logistic-regression head over frozen
[all-MiniLM-L6-v2](https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2)
embeddings. The Apache-2.0 base has approximately 22.7 million parameters; the
head has 7,705 learned parameters. This is **head training, not encoder fine-tuning**.
It needs neither llama nor a GPU. Production does not load this model.

## Data and representation

`seed.json` contains assistant-authored synthetic examples with provisional labels:
36 training scenarios and 19 development scenarios. Reversing every message's
direction doubles these to 72 and 38 examples. Both versions stay in the same split;
they are not independent observations. No private mailbox data was used.

Only message bodies and directions become features. Separate inbound/outbound
slots preserve the order of the previous and latest messages, each with its own
384-dimensional embedding and presence indicator. Inputs are limited to two
messages, 4,000 bytes each, and 256 tokens per message. Longer inputs fail rather
than silently losing context. Quoted text is retained; attachment contents are absent.

The head learns all five benchmark labels, including ambiguity/abstention. An
additional confidence cutoff maximizes development coverage with zero accepted
development errors over a fixed grid. These scores are **not calibrated probabilities**;
zero development errors do not imply zero future errors. `cases.json` is excluded
from training and cutoff selection, but remains a known development challenge set.

## Reproduce

Run from the repository root. Tested with Python 3.14.7 on x86_64 Linux. Dependencies
are pinned in `requirements.txt`; the base revision is pinned in `train.py`.

```sh
nix-shell experiments/triage/shell.nix
uv venv experiments/triage/.venv --python python3
uv pip sync --python experiments/triage/.venv/bin/python \
  --torch-backend cpu experiments/triage/requirements.txt
experiments/triage/.venv/bin/python -m unittest discover \
  -s experiments/triage -p 'test_*.py' -v
experiments/triage/.venv/bin/python experiments/triage/train.py train \
  --output experiments/triage/runs/my-pilot
HF_HUB_OFFLINE=1 experiments/triage/.venv/bin/python experiments/triage/train.py predict \
  --model experiments/triage/runs/my-pilot \
  --data experiments/triage/cases.json \
  --output experiments/triage/runs/my-pilot/predictions.json
exit
TETHER_TRIAGE_PREDICTIONS="$PWD/experiments/triage/runs/my-pilot/predictions.json" \
  nix-shell -p go --run 'go test -mod=vendor -run "^TestTriageImportedPredictions$" -count=1 -v .'
```

Choose a fresh output path; existing models/predictions are not overwritten.
Training downloads public weights; prediction uses only the saved local encoder.
No upload or inference service is configured. Training uses four CPU threads.
Artifacts include safetensors weights, a JSON head, and dataset hash/training metrics.
The completed pilot is at ignored `runs/minilm-pilot/` (about 88 MiB).
Keep future private datasets under ignored `private/`; never commit data or weights
trained from mail. CLI-created files use owner-only permissions.

## Decision

See [RESULTS.md](RESULTS.md): this pilot underperforms the rules. Keep it offline.
Continue with fully synthetic scenarios as specified in [SYNTHETIC.md](SYNTHETIC.md).
Review labels and keep each scenario family together before augmentation; reserve
separate evaluation families. Do not use existing automatic thread states as gold
labels. Compare later encoder fine-tuning against this frozen-encoder baseline.
