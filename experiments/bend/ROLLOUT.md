# Atlas eligibility rollout — 2026-09-18

## Scope and release

Source commit: `015e0a2` (includes the LLM-outage triage fix).
Bend 2.0.5 controls reply/bump eligibility. Go retains other reminder rules,
quiet hours, daily caps, Discord delivery, and confirmed-delivery logging.
Email classification and free-text chat still require the local LLM.

The operator approved isolated production-snapshot validation in place of waiting
for positive live reply/bump cases. This is a staged rollout, not evidence that
positive live notifications have already been delivered.

## Validation

- Go suite, Bend proof checks, native comparisons, and all 11 deliberate mutation
  rejections passed before deployment.
- A locked, private copy of 513 production threads and reminder history received
  eight synthetic threads: eligible, snoozed, recent, and done for both rules.
- Go, Bend, missing-evaluator fallback, and switch-removal rollback selected
  identical notifications. Each lane made three fake deliveries and persisted
  the expected confirmations. All 521 shadow comparisons agreed.
- The exact NixOS-built evaluator subsequently passed the 144-entry native test
  and all four snapshot lanes. The staging service had no network access and
  no credentials; fixtures never entered production.
- Before and after activation, live shadow checked 513 threads with no skips,
  no disagreements, and zero eligible threads. Live-positive evidence remains
  pending. Quiet hours prevent actual notification batches until daytime.

## Deployment and monitoring

NixOS generation:
`/nix/store/1sxg9hdhyml6nnhhi5yj5rfbr7jhgi6a-nixos-system-atlas-26.11.20260831.34ab990`

Wrapper: `/nix/store/pzqvn1drykpvchsgmsmda7i4gmwgpg7q-tether/bin/tether`.
Evaluator: `/nix/store/0cjsfny8dy49fjigy0x214bh4mn6m1mm-tether-bend-shadow-2.0.5/bin/tether-bend-shadow`.

The normal NixOS switch installed persistent services and timers; no manual unit
overrides remain. The bot restarted successfully. Llama remained inactive.
The empty `bend-eligibility.enabled` switch is owned by `ethoma`, mode `0600`.
The one confirmed outage-parked email was restored to `new` with zero attempts.
A direct triage run reported the unavailable LLM and left that email queued;
no other thread classifications were changed by the repair.

```sh
ssh atlas 'journalctl -u tether-pulse.service -u tether-bend-shadow.service --since today'
```

Daytime batches should log `Bend eligibility active`; evaluator failures log Go
fallback. Shadow should continue reporting `ok` without skips or disagreements.
The timer runs every 15 minutes and persists across reboot.

## Rollback

```sh
ssh atlas 'sudo -u ethoma rm /var/lib/tether/bend-eligibility.enabled'
```

The next nudge batch uses Go; an in-flight batch finishes with its selected policy.
No restart or data rollback is needed. Keep shadow monitoring on while diagnosing.
The previous source and system path are retained privately under
`/var/lib/tether-bend-rollout-20260918/` for a full application rollback if needed.

## Dispatch shadow stage — 2026-09-18

Source commit `cc5ed00` adds native dispatch batch selection with an independent
`bend-dispatch.enabled` switch. The switch remains absent in production;
eligibility remains enabled. Go retains quiet-hour and daily/candidate safety
limits and all delivery/persistence effects.

The Go suite, existing proofs and 11 mutation rejections passed. The 72-entry
native dispatch table passed 200 comparisons including inputs above the cap.
Existing dispatch and delivery-recovery tests now exercise the native production
path. The isolated 513-thread snapshot plus eight fixtures passed Go, both Bend
policies, evaluator-failure fallback, and rollback: three identical fake deliveries
per lane, matching confirmations, and no shadow disagreements. The exact
NixOS-built evaluator passed native and snapshot gates again before activation.

Deployment generation:
`/nix/store/mmrz9swv0nrh7ri4641f7f3fsj3r12if-nixos-system-atlas-26.11.20260831.34ab990`

Policy package:
`/nix/store/bm5ndi785kz65qyik8alvms0pfggvpfb-tether-bend-shadow-2.0.5`

The persistent shadow service now runs eligibility and dispatch comparisons.
First live dispatch report: `status=ok`, `quiet=true`, `fired_today=0`,
`candidates=0`, `go_batch=0`, `bend_batch=0`. Eligibility still reports 513 threads,
zero skips, and zero disagreements. Bot and timer are active; llama remains off.
Daytime dispatch observation remains pending; this deployment does not enable
Bend dispatch authority. Temporary snapshot/test files were removed afterward.

After reviewing daytime shadow evidence, create an empty
`/var/lib/tether/bend-dispatch.enabled` to activate batch selection. Remove only
that file to roll dispatch back without changing eligibility. The previous source
and system path are retained under `/var/lib/tether-dispatch-staging-20260918/`.

## Delivery observer stage — 2026-09-18

Source commit `65bcd71` adds `tether-bend-delivery` and a shadow observer inside
nonempty nudge batches. Go still controls every send, log write, and stop decision.
Dispatch authority remains disabled because daytime evidence is still pending.

The Go suite, Bend proofs, 11 mutation rejections, and all 48 native delivery
transitions passed. Delivery/retry comparisons exercise the observer and require
zero disagreements. Regression tests confirm invalid, failing, timed-out, and
semantically wrong evaluators cannot alter sending. A successful HTTP send followed
by a log-write failure stops the batch and reports zero persisted confirmations.
No observer is invoked during quiet hours or for empty batches.

The isolated production snapshot (513 real threads plus eight fixtures) passed
all four lanes with three identical fake deliveries each and zero delivery
transition disagreements. The exact NixOS-built policy package passed the native
and snapshot gates again before activation. Staging had no network or credentials;
its temporary snapshot and executable were removed after validation.

Deployment generation:
`/nix/store/3civgvpj834hlr9xz5qwgc1fml1zjx9k-nixos-system-atlas-26.11.20260831.34ab990`

Policy package:
`/nix/store/bx810xk8zq7qi6b9sd7708n0wg8f794d-tether-bend-shadow-2.0.5`

The running bot's `TETHER_BEND_DELIVERY` points to this package. Bot and shadow timer
are active; llama remains inactive. Eligibility still has 513 checks and no skips
or disagreements; dispatch remains an agreeing quiet-hour zero batch. There are
no live delivery-transition results yet. On the next nonempty daytime batch, look
for policy `bend-2.0.5/delivery-v1` in the sender's journal, usually
`tether-pulse.service`. Require zero mismatches; observer-unavailable messages are
missing evidence, not agreement.

At that deployment there was no delivery authority switch. Set `TETHER_BEND_DELIVERY` to an empty value
in the service environment and restart the bot to disable its observation; future
oneshot services reload the environment automatically. The previous source and
system path are retained under `/var/lib/tether-delivery-staging-20260918/`.

## Delivery authority implementation

Read-only journal inspection since 2026-09-18 found a daytime dispatch shadow
record with one candidate and matching Go/Bend batch sizes of one. The pulse/bot
journals contain three successful delivery observations, each checking one
transition with zero mismatches and one persisted confirmation. No unavailable
observer records appeared in that sender query. These observations satisfy the
previously pending daytime evidence; they do not establish failure-path behavior.

The sender now supports independent opt-in delivery authority through an empty,
regular `/var/lib/tether/bend-delivery.enabled` file. Before activation it checks
all 48 native transitions against Go, including skipped, failed, and successful
outcomes. A bad switch, evaluator failure, or semantic disagreement retains Go
transitions. Only successful sends followed by successful log writes advance
the confirmed index. Go still bounds attempts and returns immediately on errors.

The Go suite and native Bend gate passed, including 48 transition inputs, delivery
and retry comparisons with delivery authority enabled, and 11 rejected mutations.
Tests cover activation, rollback, invalid switches, malformed or unavailable
evaluators, semantic disagreement, and a successful send followed by log failure.
The snapshot gate exercises all three switches and independently verifies delivery
backend selection through activation, unavailable-evaluator fallback, and rollback.

After deployment and its isolated snapshot gate, enable with
`sudo -u ethoma touch /var/lib/tether/bend-delivery.enabled`; remove only that file
to restore Go delivery transitions for the next batch. Dispatch has its own
`bend-dispatch.enabled` switch. Neither command sends a notification. These source
changes and journal observations alone do not claim deployment or activation.

### Exact packaged snapshot gate — 2026-09-18 23:45 UTC

The new Go sender/test binary passed against atlas's installed policy package:
`/nix/store/bx810xk8zq7qi6b9sd7708n0wg8f794d-tether-bend-shadow-2.0.5`.
Native eligibility, dispatch, and all 48 delivery transition checks passed.

A private snapshot copied under `/var/lib/tether/lock` contained 534 production
threads. The test added eight synthetic fixtures inside its own temporary stores.
All four lanes (Go, three Bend switches enabled, missing-evaluator fallback, and
switch-removal rollback) selected two identical fake deliveries and persisted two
confirmations. All 542 eligibility comparisons agreed without skips. Dispatch
selected two of two candidates with three prior confirmations, preserving the
daily limit of five. Delivery reported two checked transitions, zero mismatches,
and two confirmations in each available-evaluator lane; the unavailable-evaluator
lane retained Go and returned the same result.

The transient test unit ran as `ethoma` with `PrivateNetwork`, `PrivateTmp`,
`ProtectSystem=strict`, `ProtectHome`, `NoNewPrivileges`, and the live state path
inaccessible. It received only evaluator/snapshot paths, no credential environment.
HTTP used fake transports. Runtime was 179 ms; peak memory 14.1 MB. The private
snapshot and test executable were removed after the gate. No switches or live
state were changed. Deployment remains the separate next step.
