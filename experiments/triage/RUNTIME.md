# CPU inference runtime

`triage-runtime.nix` packages the already installed, pinned experiment dependencies
and the two inference scripts into an immutable Nix closure. Model weights and
mail data remain outside that closure. The package does not download dependencies
or load models during its build.

Prepare `.venv` using the experiment setup and `requirements.txt`, then supply its
absolute location and its immutable Nix Python executable explicitly:

```sh
nix-build ./triage-runtime.nix \
  --argstr runtimeSource "$PWD/experiments/triage/.venv" \
  --argstr pythonExecutable "$(readlink experiments/triage/.venv/bin/python)" \
  --out-link experiments/triage/runs/runtime-v2
```

The default Python library directory is `python3.14`; change `--argstr pythonVersion`
only when rebuilding the pinned environment with a different interpreter. Use the
same nixpkgs selection as `experiments/triage/shell.nix` for the shared libraries.
This snapshots the installed environment; `requirements.txt` alone is not a
content-hashed wheel lock. Record the resulting store path and smoke-test the
actual model before rollout.

Invoke `bin/tether-triage-shadow --model /absolute/model/path` with the bounded
JSON request on stdin described in `SHADOW.md`. The wrapper selects CPU inference,
four computation threads, offline model loading, and its own Python dependencies.
It copies only site-packages, `train.py`, and `shadow.py`; it does not copy virtual
environment entrypoints or depend on a home-directory path.

Transfer the output with `nix copy --to ssh-ng://atlas STORE_PATH`. The closure
includes the original Python executable, shared libraries, and packaged wheels.
Root the output on the destination (for example, through the deployed service's
Nix derivation) before garbage collection. Keep the model directory private and
test the adapter under the service user and network isolation before enabling its
timer. Keep the previous runtime and model rooted for rollback.
