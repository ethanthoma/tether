# V2 CPU classifier rollout

This records the first rejected candidate and its read-only deployment. For the
later approved classifier, see [the v2j production record](../v2j/PRODUCTION.md).

## Inputs and scope

The release uses 893 training cases from 134 approved v2 families, plus 120 new
development cases and 120 new test cases from 30 families each. All are synthetic.
A separate blind context reviewed every evaluation case without author labels;
all agreed. This is model-context review, not human ground truth. The 14 disputed
training families remain excluded. Partition separation checks pass against the
training corpus and previous evaluation datasets.

The [release gate](RELEASE.md) was committed before fitting or viewing predictions.
The 12-epoch CPU recipe selects its checkpoint and confidence threshold on dev
only. Recorded test results and deployment evidence follow when available.

## Runtime controls

`TETHER_TRIAGE_SHADOW` names the trusted CPU inference wrapper. The production
adapter only accepts `reply-triage-v2` and an exact artifact hash matching the
64 lowercase hex characters in `/var/lib/tether/triage-model.enabled` (one optional
newline). It classifies only new threads, at most 20 per run. It sends no mail
and does not replace the general-purpose assistant or extract commitments.

Abstentions, rejected context, missing artifacts, invalid switches, hash/policy
mismatches, and subprocess failures retain the existing LLM triage path. If llama
is unavailable, undecided threads remain new without consuming retry attempts;
accepted model results elsewhere in the batch still apply. The batch rotates
between runs while a model switch exists so unresolved threads cannot permanently
block the rest of the queue.

Inputs use the latest two cached messages, bounded at 4,000 bytes per body and
256 encoder tokens per message; oversized inputs are rejected rather than
silently truncated. The subprocess has a 30-second deadline. Its environment
omits service credentials and loads model files offline on CPU.

## Operations and rollback

`tether-triage-shadow.service` runs read-only comparisons every 30 minutes with
network isolation, a two-GiB memory limit, and four CPU equivalents. Its reports
contain opaque thread hashes and aggregate coverage; stored classifications are
not accuracy labels. Keep reports private and inspect failures and rejected-input
rates separately from synthetic test metrics.

The model directory is `/var/lib/tether-model/current`; the system closure roots
the immutable runtime. Keep previous versioned model directories for rollback.
Remove only `/var/lib/tether/triage-model.enabled` to stop model authority on the
next triage invocation. Roll back the NixOS generation for code/runtime rollback.
Bend's eligibility, dispatch, and delivery switches are independent of this model.

## First candidate result

The first run selected epoch two, with raw dev accuracy 68/120 (56.7%). Every
non-abstaining threshold on the frozen grid accepted at least one wrong dev
prediction. The selected threshold is therefore 1.0: **zero accepted cases**.
This candidate cannot meet the 25% release coverage requirement and is rejected
for authority. Test predictions were not run; the cutoff guarantees abstention
regardless of test content. The complete history is in `training-report.json`.

The runtime may be deployed for read-only observation with this artifact, but
`triage-model.enabled` must remain absent. Improving training coverage and
retesting under a separately recorded release plan is required before activation.

## Production read-only deployment — 2026-09-19

Committed source `d2eb9d8` was deployed from an archive and built before switching.
Activated NixOS generation:
`/nix/store/6bv3dgxmkbs2a6m7f3idvwi9zrasdlsy-nixos-system-atlas-26.11.20260831.34ab990`.
The system closure roots runtime
`/nix/store/mk3cxyh659zj57xp0giw39gyv88j0mkg-tether-triage-runtime-v2`.

Artifact identity:
`078b9eec37e2a3fc05b3c69e5b37d5d4728440a779b9882373ec11e429a03fe1`.
Its versioned directory is `/var/lib/tether-model/models/<identity>`, with `current`
pointing there. The parent and models directories are `root:users`, mode `0750`;
artifact directories are `0550`, files `0440`. The service user cannot write the
parent, model directory, or contents. No credentials or message data enter the
runtime closure. The authority switch remains absent: this candidate is still
rejected for live classification.

Before timer activation, the exact runtime passed a synthetic smoke request as
`ethoma` with network isolation, no credential environment, and live state
inaccessible. It returned the expected v2 policy, exact artifact identity, and an
abstention. Cold runtime was 11.533 seconds and peak memory 371.6 MiB.

The production shadow report at `2026-09-19T06:56:41Z` recorded 15 checked cases,
519 skipped threads, 15 abstentions, and five token-rejected inputs. Duration was
11,779 ms; service peak memory was 470,556,672 bytes. The unit exited successfully.
There were zero classification comparisons because every prediction abstained;
this is plumbing and input-coverage evidence, not production accuracy evidence.
The full report remains private, owned by root with mode `0600`, under
`/var/lib/tether-classifier-staging-20260919/shadow-report.json`.

The bot and pulse, digest, Bend shadow, and classifier shadow timers are active.
Llama remains inactive. All three independent Bend switches remain enabled.
The rebuilt Bend executables have different binary bytes, but all three emitted
policy tables match the previously validated package byte-for-byte. A live Bend
shadow run checked 534 threads with zero skips or disagreements; dispatch agreed
on the quiet-hour zero batch. No manual notifications or nudge runs were invoked.

The prior Bend-enabled source and generation remain privately backed up in
`/var/lib/tether-classifier-staging-20260919/` as `source-before` and `system-before`.
To stop observation, disable `tether-triage-shadow.timer`; to roll back the code,
restore that source and switch to the recorded generation. Do not create
`triage-model.enabled` for this artifact.
