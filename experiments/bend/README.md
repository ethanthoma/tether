# Bend follow-up experiment

This isolated model evaluates Bend's proof workflow against Tether's follow-up
rules. A bounded comparison checks representative inputs against Go; it does
not replace or formally verify the Go application.

## Run

```sh
./experiments/bend/check.sh
# Or: nix-shell -p gnumake --run 'make test-bend'
```

Requires Nix and network access. The script supplies Bun and download tools through
`nix-shell`, downloads Bend **2.0.5**, verifies its pinned SHA-256, and runs the
compiler directly. It uses a temporary directory and removes it on exit; it does
not use Bend's auto-updating launcher or change shell configuration.

Success prints `All terms check.`, passing Go/Bend comparisons, and eleven regression rejection messages.
The script rejects failures unrelated to the expected law, so a syntax error
does not count as catching a regression.

## Model and laws

- `follow_up.bend`: six thread states, outbound invalidation, cooldown, and repeat eligibility.
- `dispatch.bend`: quiet hours, daily budgets, and bounded delivery-state transitions.
- `LAWS.bend`: seventeen requirements, separate from their proofs.
- `PROOF.bend`: proofs by reduction and case analysis.
- `shadow.bend`: native entry point emitting the policy's 144-entry decision table.

The laws establish that every outbound message requires triage, every snoozed,
unclassified, or completed thread is silent, and an overdue unsnoozed request can
produce a reminder after cooldown. Reply reminders stop at two fires in the active
history window; the first and second remain eligible. Bump reminders have no count
limit. Positive laws prevent an implementation that simply disables all reminders
from satisfying the specification.

Dispatch laws establish that quiet hours and exhausted daily budgets produce no
notifications, and batch size never exceeds `max(5 - fired_today, 0)`. A positive
law requires using all five slots when ten candidates are available and none have
been sent. Proofs use finite cases at the fixed cap; no recursive proof is needed.

Delivery laws establish that failure stops further attempts without incrementing
the confirmed count, success increments that count once, and skipped slots preserve
state. `deliver` composes five guarded steps, matching the fixed daily cap.

The runner deliberately introduces eleven bugs in temporary copies: automatically
awaiting a reply after sending, nudging snoozed or unclassified threads, bypassing
cooldown, exceeding the reply limit, ignoring quiet hours, sending after the daily
cap, overestimating the remaining allowance, resuming after failure, counting failed
deliveries, and dropping successful counts. Each must fail its corresponding proof
with the original laws unchanged.

## Boundaries

`bend_test.go` runs the actual Bend model and compares it with Go's `collectNudges`
for all 24 combinations of six states, snooze, and overdue, crossed with 16 history
fixtures: **384 eligibility cases**. It also compares all six outbound transitions
with `ApplyOutbound`. Go timestamps are exactly at each rule's deadline for
`overdue=false`, and one nanosecond past it for `true`.

History fixtures cover zero through three fires; just before, exactly at, and just
after cooldown; and just before and exactly at the 90-day history expiry. Recent
entries for another thread or rule must not interfere. No events, contacts,
commitments, or manual reminders are present.
Run this optional test through `check.sh`; normal `make test` does not require Bend.

`bend_dispatch_test.go` compares **175 dispatch cases** against `RunNudges`: seven
local clock times around midnight, 08:00, and 22:00; five prior daily counts; and
five candidate counts. A fake HTTP transport intercepts every Discord request, so
no real notifications are sent. Tests also check persisted daily counts and that
entries one nanosecond before local midnight do not consume today's allowance.

Another **216 failure/recovery scenarios** compare **432 outcomes**: confirmed
counts, returned errors, and attempt counts before and after retry. They cover HTTP
429 and 503 responses and simulated connection failures, at three positions, with
different budgets, queue lengths, and quiet-hour states. The real Go loop writes to
temporary stores. Failed attempts must not be logged; retries must retain the daily
budget and avoid repeating already logged reminder IDs.

`snoozed`, `recent`, and `overdue` are supplied Booleans; `fires` is the count in the
active history window. Dispatch takes a supplied `quiet` Boolean and daily count.
Delivery takes the number of successes possible before a definite failure.
The Go comparisons exercise timestamp boundaries and successful log writes, but
Bend does not prove timestamp filtering or persistence itself. These simulated
failures occur before delivery. A timeout after Discord accepts a message, or a
crash before the corresponding log write, can still cause duplicates on retry;
exactly-once delivery is not established. Disk failures, crash recovery, concurrency,
and LLM classifications remain outside these models.
The proofs cover all inputs to the stated laws. The comparison establishes agreement
only for the enumerated fixtures, not a proof of the Go implementation or its other
notification rules.

The runner also compiles `shadow.bend` with Clang and checks all 144 native table
entries against Go, followed by a complete read-only shadow comparison. The Nix
`bend-shadow` package runs proof checking before compilation and validates the output
protocol. `shadow_test.go` exercises execution failures, timeouts, output bounds,
invalid protocols, credential isolation, and disagreement reporting without writes.

No parallelism is used: these decisions are tiny branch operations. This experiment
measures proof usability, not CPU/GPU speed. Bend's CLI has no formatter command;
the files follow its documented two-space layout. Format the shell runner with
`nix-shell -p shfmt --run 'shfmt -w experiments/bend/check.sh'`.

References: [Bend guide](https://github.com/bendlang/bend/blob/main/guide/GUIDE.md),
[pinned release](https://bend-lang.com/dl/2.0.5.tar.gz).
