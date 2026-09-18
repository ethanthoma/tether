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
