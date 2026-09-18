# tether

Single-user comms hub: syncs Gmail (IMAP) + Google Calendar (secret ICS URL)
into a JSON state dir on atlas, triages threads with the local llama-server,
extracts commitments, and reaches you through a Discord bot that both posts
nudges and takes commands.

```
tether sync                        fetch new mail + calendar
tether triage                      LLM-classify new threads, extract commitments
tether nudge                       evaluate nudge rules, post to Discord
tether digest                      post morning digest to Discord
tether pulse                       sync + triage + nudge (what the timer runs)
tether bot                         run the Discord bot (long-running service)
tether ask "who am I ghosting?"    question over the store
tether list [threads|commitments|contacts|reminders]
tether done <id>                   close thread / keep commitment / finish reminder
tether snooze <id> 3d
tether track alice@example.com 30
tether remind "renew passport" 2026-09-01
```

## Discord

Every CLI command is a slash command: `/list`, `/ask`, `/done`, `/snooze`,
`/remind`, `/track`, `/digest`, `/sync`, `/triage`, `/pulse`. Discord supplies
the argument names, types, and `/list`'s choice menu, so the options are
discoverable as you type.

Slash commands arrive as *interactions*, which Discord POSTs to an HTTPS
endpoint — they are not messages, and cannot be read from the message API. So
`tether bot` runs an HTTP server on `TETHER_BOT_ADDR` (default
`127.0.0.1:8082`), published as `tether.gaugenumerics.com` through the atlas
cloudflared tunnel. Every request is verified with the app's ed25519 public key
(`crypto/ed25519`, no dependency), and commands from any channel other than
`TETHER_DISCORD_CHANNEL` are refused.

Discord requires a reply within 3 seconds, so the server acks immediately with a
deferred response and edits in the real answer when the work finishes. That
matters because `/pulse` and `/ask` take minutes, and because a command can
block on the store lock while `tether-pulse` holds it.

Commands are registered at startup — guild-scoped when the bot is in exactly one
server (instant), global otherwise (up to an hour to propagate).

Setup: discord.com/developers → New Application → Bot → Reset Token
(`TETHER_DISCORD_TOKEN`). General Information → Public Key
(`TETHER_DISCORD_PUBLIC_KEY`). Set **Interactions Endpoint URL** to
`https://tether.gaugenumerics.com/` — Discord validates it with a signed PING on
save, so the service must already be running. Then OAuth2 → URL Generator →
scope `bot`, permissions *View Channel* + *Send Messages* (68608 also covers
*Read Message History*) → open the URL and add it to your server.
`TETHER_DISCORD_CHANNEL` is the channel id (Developer Mode on, right-click the
channel → Copy Channel ID). Message Content Intent is no longer needed.

## Configuration (environment)

| var | meaning |
|---|---|
| `TETHER_STATE_DIR` | state dir, default `/var/lib/tether` |
| `TETHER_IMAP_USER` | Gmail address |
| `TETHER_IMAP_PASSWORD` | Gmail app password (myaccount.google.com/apppasswords, needs 2FA) |
| `TETHER_MY_EMAIL` | defaults to `TETHER_IMAP_USER` |
| `TETHER_ICS_URL` | Calendar settings → "Secret address in iCal format" |
| `TETHER_DISCORD_TOKEN` | bot token |
| `TETHER_DISCORD_CHANNEL` | channel id the bot answers and posts in |
| `TETHER_DISCORD_PUBLIC_KEY` | app public key, verifies interaction signatures |
| `TETHER_BOT_ADDR` | interactions listen address, default `127.0.0.1:8082` |
| `TETHER_LLM_URL` | default `http://127.0.0.1:8080` |
| `LLAMA_API_KEY` | same value as in `/var/lib/llama-server.env` on atlas |

Secrets are declared in `secretspec.toml` (keyring provider, KeePassXC via
Secret Service, same setup as the website repo). `secretspec check` populates
missing entries, then `make provision` renders `/var/lib/tether.env` on atlas
(owned `ethoma`, mode 600), pulling `LLAMA_API_KEY` from
`/var/lib/llama-server.env` there. The packaged `tether` command sources that
file, so the services and `ssh atlas tether ask` behave identically. Everything
stays dormant until the file exists.

## Deploy

`make deploy` rsyncs the source to `atlas:/etc/nixos/tether/`; atlas's
configuration.nix builds it with `pkgs.buildGoModule { vendorHash = null; }` and
runs `tether-bot.service` (always on), `tether-pulse.timer` (every 15 min), and
`tether-digest.timer` (07:00).

The bot holds no lock while idle: it opens the state dir only while running a
command, so it never blocks the pulse timer. At most 4 commands run at once.

DNS for `tether.gaugenumerics.com` is `cloudflare_record.atlas_tether` in
`~/projects/website/ops/dns.tf`; the tunnel ingress is declared in `tether.nix`
and merges into the tunnel that also serves llama-server.

## Dev

`make build`, `make test`. Local runs: `TETHER_STATE_DIR=/tmp/tether-dev ./tether sync`.
On this machine, prefix with `nix-shell -p go --run '...'`.

## Bend shadow trial

`tether shadow` compares Go's thread reminder eligibility with the verified Bend
policy. It reads the store and nudge history, emits a JSON report, and sends nothing.
Set `TETHER_BEND_SHADOW` to the absolute native evaluator path. Build both packages
with `nix build path:.#default path:.#bend-shadow --no-link --print-out-paths`.

`tether.nix` defines a separate `tether-bend-shadow.timer` running every 15 minutes.
The service has no credentials or network access and mounts the state read-only
except for its shared lock. It compares against Go independently of the production
eligibility switch. View reports with `journalctl -u tether-bend-shadow.service`.
Stop future runs with `sudo systemctl stop tether-bend-shadow.timer`; any current
run finishes within the service's ten-second limit. Remove the timer's `wantedBy`
entry before a NixOS rebuild to keep it disabled.

The initial Atlas trial used transient units. The September 18 deployment installed
persistent NixOS units and enabled eligibility after the approved snapshot gate.
NixOS generations now retain the package closures; the temporary trial GC roots
are no longer needed. See [the rollout record](experiments/bend/ROLLOUT.md) for
validation, deployed paths, monitoring, and rollback.

Reports contain checked/skipped counts, disagreement counts, Go/Bend eligible
counts, sorted distinct `cases_seen`, up to eight short thread IDs, and elapsed
microseconds. Repeated checks of the same cases do not establish new coverage.
Evaluator errors fail only the observer.
Runs cap the evaluator at two seconds, the snapshot at 4,096 threads, and the
nudge log at 8 MiB. `partial` means some threads were skipped. The service timeout
also bounds waiting for the store lock. No email text reaches the evaluator.

The native evaluator emits all 144 combinations of the finite eligibility policy;
Go indexes that table with live inputs. Bun and Clang are build dependencies only.
This checks reply/bump eligibility, not message classification or the final dispatch
decision. See [the experiment](experiments/bend/README.md) for proof coverage and tests.

Before production authority, require passing native comparisons and failure tests,
and no unexplained observer errors, skips, or disagreements. Positive and suppressed
reply/bump cases must pass either live observation or an explicitly approved staging
gate against an isolated production snapshot with controlled fixtures. Staging
evidence is not live-positive evidence. The trial does not promote itself.
Case IDs encode `state * 24 + flags * 3 + fires`:
states are new, needs-reply, waiting-on-them, FYI, noise, done; flag bits are snoozed,
recent, overdue; fires is capped at two. The live input mapping normalizes irrelevant
fields, so only 36 of the 144 native table entries are reachable. Missing live cases
remain supported by offline tests, not production evidence.

### Production switch (disabled by default)

The Nix service wrapper supplies the pinned native evaluator. After deploying
this version **and passing the eligibility gate above**,
create an empty `/var/lib/tether/bend-eligibility.enabled` file to enable Bend
eligibility. For an isolated store, use the same filename in `TETHER_STATE_DIR`.
No enable file is created by packaging or deployment.

```sh
sudo -u ethoma touch /var/lib/tether/bend-eligibility.enabled
# Roll back eligibility on the next nudge run; no bot restart needed.
sudo -u ethoma rm /var/lib/tether/bend-eligibility.enabled
```

The switch is read once per nudge batch; an in-flight batch completes using its
selected policy. Bend controls only reply/bump eligibility. Go retains other
reminders, quiet hours, daily limits, sending, and confirmed-delivery logging.
Evaluator errors, invalid output, a two-second timeout, unknown thread states,
an invalid switch file, or more than 4,096 threads fall back to Go for the entire
batch and log the reason. Shadow comparison always uses the independent Go
policy, even when production Bend eligibility is enabled.

Successful Bend batches log `Bend eligibility active`; fallback batches log their
reason. Quiet hours return before invoking either eligibility backend. The local
LLM is not required for Bend, buttons, or already classified threads. New email
classification and free-text chat still require it; service outages preserve the
triage queue without consuming classification attempts.

### Isolated production snapshot gate

Copy `threads.json`, `contacts.json`, `commitments.json`, `reminders.json`,
`calendar.json`, `sync.json`, and `nudges.jsonl` while holding the production
`lock`. Keep the snapshot private and on the production host; omit credentials
and message bodies. Compile the tagged test binary locally and run only
`TestBendProductionSnapshot` in a service with `PrivateNetwork=yes`:

```sh
CGO_ENABLED=0 go test -mod=vendor -tags bend -c -o /tmp/tether-bend-check.test
TETHER_BEND_SNAPSHOT=/private/snapshot \
TETHER_BEND_SHADOW=/nix/store/.../bin/tether-bend-shadow \
  /tmp/tether-bend-check.test -test.run '^TestBendProductionSnapshot$' -test.v
```

The test copies metadata into disposable stores, adds eight controlled threads,
and compares complete nudge selections across Go, Bend, evaluator-failure fallback,
and switch-removal rollback. Reply/bump positives, snoozes, cooldowns, and done
states are checked explicitly. Delivery uses a fake HTTP transport; daily caps
and persisted confirmations must agree. Real production state is never modified.
Delete the private snapshot after validation. No fixtures belong in production.

### Bend dispatch migration

`shadow-dispatch` compares Go and Bend batch sizes without sending or changing
state. The existing shadow service runs both eligibility and dispatch checks.
Dispatch reports include `quiet`, `fired_today`, `candidates`, `go_batch`, and
`bend_batch`; a disagreement changes `status` to `mismatch`.

`TETHER_BEND_DISPATCH` selects the native `tether-bend-dispatch` executable,
packaged alongside the eligibility evaluator. Its versioned table contains 72
single-digit counts: quiet (2) × prior deliveries (6) × candidates (6). Counts
above five map to five because the daily cap is five. No LLM or Bun runs here.

Dispatch authority is **disabled by default**. After native/failure tests, the
isolated snapshot gate, and production shadow review, create an empty
`bend-dispatch.enabled` in the state directory to let Bend select the batch size.
Removing it restores Go batch selection on the next run without affecting Bend
eligibility. Keep the two switches separate during rollout.

Go's quiet-hour guard and daily/candidate upper bounds remain enforced. An invalid
switch, evaluator error, timeout, malformed table, or an excessive proposed batch
falls back to Go. Sending stops on the first failed delivery or log write; only
confirmed sends enter the nudge log. Successful batches log `Bend dispatch active`.
The snapshot test now requires both evaluator environment variables and checks
both dispatch and eligibility through activation, failure fallback, and rollback.
