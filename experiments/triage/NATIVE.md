# Isolated production classifier gate

`TestTriageNativeProduction` invokes the real, bare Go CLI and trusted model wrapper
against fresh temporary stores containing only synthetic messages. It tests exact
artifact activation, wrong-hash fallback, absent-switch fallback, and switch-removal
rollback. A loopback HTTP server returns 503 for every LLM request. Assertions check
persisted classifications, unchanged queued-thread notes and retry counts, and no
commitment extraction. No production store is opened and no credentials are passed
to the CLI subprocess.

Build with `go test -mod=vendor -c -o /absolute/gate.test .`, then run only
`gate.test -test.v -test.run '^TestTriageNativeProduction$'` with:

- `TETHER_TRIAGE_NATIVE`: absolute trusted inference wrapper path.
- `TETHER_TRIAGE_NATIVE_BINARY`: absolute **bare** `tether` executable; do not use the
  service wrapper that reads `/var/lib/tether.env`.
- `TETHER_TRIAGE_NATIVE_HASH`: exact 64-character lowercase artifact hash.
- `TETHER_TRIAGE_NATIVE_CASES`: optional absolute synthetic smoke packet path.

Run the test binary as the production user inside a transient systemd unit with
`PrivateNetwork=true`, `PrivateTmp=true`, `ProtectHome=true`,
`ProtectSystem=strict`, `NoNewPrivileges=true`, and
`InaccessiblePaths=/var/lib/tether`. Supply only the four gate variables, no service
credential environment. The unit needs read access to the immutable runtime,
versioned model directory, and optional synthetic packet. Allow 120 seconds and
2 GiB memory. The test's child commands each have a 45-second deadline.

Without a packet the test expects one abstention, useful for plumbing verification
of the rejected threshold-1 artifact. This **does not prove accepted classification**.
After a candidate passes its recorded release gate, provide at most 20 independently
specified synthetic cases, including at least one accepted classification and one
abstention:

```json
{
  "provenance": "fully_synthetic",
  "cases": [
    {"expected": "needs_reply", "messages": [{"outbound": false, "body": "Which date should I put on the invitation?"}]},
    {"expected": "abstain", "messages": [{"outbound": true, "body": "I'll repair the fence tomorrow."}]}
  ]
}
```

This illustrates the packet format, not a model-approved smoke fixture. Expected
labels must be fixed before running the gate; do not change them to match outputs.
Passing this integration gate does not replace the model-quality release gate.

## First artifact verification

The deployed v2 threshold-1 artifact passed all four lanes on atlas using the real
packaged CLI and runtime: one synthetic case, zero accepted, one abstained. Total
runtime was 22.868 seconds and peak memory 376.9 MiB. The transient unit had no
external network or production-state access; its HTTP traffic was solely the
loopback 503 fixture. Temporary gate files were removed. Production authority
remained absent. Accepted-classification integration is still pending a candidate
that passes the release gate.
