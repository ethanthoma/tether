# V2j production deployment

Classifier authority was enabled on Atlas at **2026-09-19 02:37:34 PDT** after
the synthetic release gate, isolated native integration, and live read-only shadow
passed. Llama remains off. Bend's eligibility, dispatch, and delivery switches
remain independently enabled.

## Immutable identities

- Deployed source: `384c91aadd5c2ecb7dd0e9e52f0e72b318966848`.
- Model: `4ac8bcd6afedea4f11fb44c5d23926f4ad1118eb740a4c7842d44b9f717ffab2`.
- Runtime: `/nix/store/yic2j3a431vavclskbwi1ay7c0mbdvwc-tether-triage-runtime-v2`.
- Generation: `/nix/store/nsd0kgjbsklfyv9m6hpfg41w54wk0967-nixos-system-atlas-26.11.20260831.34ab990`.
- Trusted wrapper: `/nix/store/j96j70v4pgy0jl14dlvlm5dcbr8rym5l-tether-triage-model`.

`/var/lib/tether-model/current` points to `models/<model hash>`. Version directories
are `root:users` mode `0550`; files are `0440`. The service user cannot replace
weights or the parent link. The NixOS closure roots the runtime.
`/var/lib/tether/triage-model.enabled` contains exactly the approved 64-character
hash, is owned by `ethoma:users`, and has mode `0600`. Activation was atomic.

## Evidence

The [held-out gate](RESULT.md) accepted 64/198 synthetic cases with zero errors,
including both actionable labels. All 69 Python tests passed; the subsequent
cutoff-only change passed its five focused regression tests. An independent
static audit reproduced report metrics and checked input hashes and smoke labels.

The exact runtime and approved model passed the isolated bare-CLI gate with five
fixed synthetic cases: four accepted and one abstained. Exact-hash activation,
wrong-hash rejection, absent-switch fallback, and switch-removal rollback passed.
Total runtime was 25.670 seconds; the active lane took 13.15 seconds. Peak memory
was reported as 648.4M. Network isolation allowed only the loopback 503 fixture;
production state and credentials were unavailable. Go source was unchanged from
the `c371230` native-gate build.

The runtime also passed the maximum-context dummy preflight: 20 exchanges of
512 tokens in 18.771 seconds, below the 30-second and 2-GiB limits. That dummy
preflight establishes resource fit, not model quality.

Live read-only shadow at `2026-09-19T09:36:51Z` verified the exact hash and v2
policy, checked 15 threads, skipped 519, and abstained on all 15; five inputs
exceeded the token limit. Duration was 15,370 ms and peak memory 1,255,714,816
bytes. Exit status was zero. There were no comparisons with existing labels.
Thus the initial live sample shows **zero accepted coverage**, not measured
real-mail accuracy. The short synthetic benchmark does not resolve that limitation.
The full private report remains under the rollout backup directory.

The bot and all timers are active. No manual pulse, nudge, or test notification
was sent. Ordinary post-activation pulse evidence is pending the next scheduled run.

## Runtime behavior and rollback

The classifier considers at most 20 new threads per run, using their latest two
messages and a combined 512-token bound. It never extracts commitments. Accepted
labels apply even while llama is unavailable; abstentions, rejected input, and
operational failures retain the existing fallback. Undecided threads stay queued
without consuming retries during an LLM outage. Free-text chat and commitment
extraction still require the LLM.

Remove `/var/lib/tether/triage-model.enabled` to stop classifier authority on the
next triage invocation. This does not undo already saved classifications. Leave
the three independent Bend switches intact.

Source, system, and model-link backups are in
`/var/lib/tether-v2j-rollout-20260919/{source-before,system-before,model-before}`.
For full rollback, remove authority first, stop the shadow timer and wait for its
unit to finish, atomically restore the prior model link, switch to the saved
generation, restore the source, then resume shadow observation. The prior schema-1
model requires its matching earlier runtime; never switch the model link alone.
Use `nixos-rebuild switch --no-reexec --store-path <saved generation>`.
