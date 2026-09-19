# Read-only CPU triage shadow

`tether triage-shadow` runs the local MiniLM classifier on cached messages without
changing threads, extracting commitments, sending reminders, or calling the LLM.
It is a separate command, also configured on a production timer every 30 minutes.

Reports identify the artifact's policy: historical `synthetic-obligations-v1`
sets `policy_aligned: false`, while `reply-triage-v2` sets it true. Policy alignment
describes the label contract, not model quality. Shadow observations are diagnostic,
not permission to classify live threads. See the [v2 rollout](v2/PRODUCTION.md)
for quality gates and the separate hash-pinned authority switch.

## Run locally

Build the Go CLI, then enter the existing pinned Python environment. Install its
dependencies as described in [TRAINING.md](TRAINING.md) if needed. A saved model
directory must contain schema-3 `head.json` and a complete local `encoder/`, including
`model.safetensors`. Current inference requires `joint-thread-v1`, class-specific
cutoffs, and a saved 512-token context limit. Historical artifacts use their pinned
older runtimes. Training artifacts remain ignored.

```sh
nix-shell -p go gnumake --run 'make build'
nix-shell experiments/triage/shell.nix
experiments/triage/.venv/bin/python - <<'PY'
import shlex
import shutil
import sys
from pathlib import Path

root = Path.cwd()
model = root / "experiments/triage/runs/v2d-joint/model"
wrapper = root / "experiments/triage/runs/shadow-infer"
with wrapper.open("x") as output:
    output.write(
        f"#!{shutil.which('bash')}\n"
        f"exec {shlex.quote(sys.executable)} "
        f"{shlex.quote(str(root / 'experiments/triage/shadow.py'))} "
        f"--model {shlex.quote(str(model))}\n"
    )
wrapper.chmod(0o700)
PY
export TETHER_TRIAGE_SHADOW="$PWD/experiments/triage/runs/shadow-infer"
TETHER_STATE_DIR=/absolute/path/to/a/scratch-store ./tether triage-shadow
```

Use a trusted absolute executable path. The wrapper uses `exec` so the timeout
controls the inference process. Run inside the Python Nix shell: its library path
is the only inherited runtime path passed through to inference. Credential
environment variables are omitted. Model loading is local-only with offline flags;
the subprocess is not an OS network sandbox. No llama server or GPU is required.

The command uses normal exclusive store locking until it exits. Thread/message
JSON remains unchanged; opening a store still creates its normal lock and cache
directories when absent. Keep any real-mail reports private.

## Bounds and report interpretation

The command considers the first 4,096 stored threads, excludes done/invalid
threads, and chooses up to 20 most recently active candidates, with ID tie-breaking.
It reads only each candidate's last two message IDs in their stored order.
Missing, malformed, empty, invalid-UTF-8, or oversized cached context skips the
whole thread. Cache files are limited to 64 KiB and bodies to 4,000 bytes.
It does not truncate bodies or fill skipped slots from older candidates.

Inference takes one batch per subprocess, with a 30-second deadline, 256 KiB
request bound, and 32 KiB output bound. A combined exchange exceeding 512 tokens,
including speaker markers, produces `input_rejected` and abstains; other cases
in the batch still run.
Timeouts, unavailable artifacts, malformed output, and nonfinite confidence fail
the command with a sanitized error. The command never converts failures into FYI.

Reports include:

- Model artifact hash, policy marker, and end-to-end duration including cold load.
- `checked`: cases returned by inference, including token-rejected cases.
- `skipped`: all stored threads not passed to inference, including done threads,
  selection limits, and invalid cached context.
- `abstained` and `input_rejected`: separate coverage and input-limit diagnostics.
- Per-case prediction, raw maximum class probability, stored state, and SHA-256
  thread identifier. No subject, address, message body, or raw thread ID is emitted.
- `comparisons` and `disagreements`: non-abstaining predictions compared with
  existing classifications. New threads are excluded. Stored state is not ground
  truth; these counts are **not accuracy or error-rate estimates**.

The cutoffs are the artifact's existing per-class development selections. Raw maximum
probability is not a calibrated guarantee, and can accompany an abstention.
Artifact identity covers the head and every encoder file, including tokenizer and
configuration, with deterministic length-prefixed hashing described in `shadow.py`.

## Validation and adoption boundary

The [synthetic smoke report](shadow-smoke-report.json) records the actual CLI-to-model
run: five synthetic fixtures, four checked, one byte-limit skip, one token-limit
rejection, and unchanged cached JSON. Duration was 4,067 ms on this development
machine; this is a small cold-start smoke run, not a production throughput benchmark.
The fixtures exercised plumbing, not a fresh model-quality evaluation.

Go tests cover read-only state, exact message context, privacy, selection bounds,
credential isolation, timeout, and adversarial evaluator output. Python tests cover
artifact identity, input rejection, threshold handling, and sanitized failures.
All 34 Python tests and the Go suite passed, with Ruff and gofmt checks clean.

The production service uses the [immutable CPU runtime](RUNTIME.md), offline model
files, a private network, and read-only state except for the normal store lock.
Before live authority, require the frozen v2 quality gate and inspect production
coverage and failures. Installing the shadow service does not enable authority.
