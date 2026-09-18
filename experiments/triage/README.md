# Email triage evaluation

Run the opt-in evaluation against an available model endpoint:

```sh
TETHER_LLM_URL=http://127.0.0.1:8080 \
  nix-shell -p go --run 'go test -mod=vendor -tags triage_eval -run "^TestTriageEmailExamples$" -count=1 -v -timeout 10m .'
```

Supply `LLAMA_API_KEY` in the environment if the endpoint requires authentication.
The test calls the model, uses temporary stores, and sends no notifications.
Normal `make test` stays offline.

## Cases and expectations

`triage_eval_test.go` contains six anonymized, paraphrased cases derived from a
read-only review of stored email history, plus one synthetic inbound request.
No original addresses, identifiers, links, tracking numbers, or message bodies
are included. Expected labels are provisional reviewer judgments, not user-approved
ground truth.

| Case | Expected state | Reason |
| --- | --- | --- |
| Shipment answered, user sends thanks | FYI | The request was resolved, including in quoted history. |
| Calendar invitation | FYI | Calendar RSVP is not a prose reply owed to a person. |
| Optional app permission update | FYI | The notice explicitly permits ignoring it. |
| Thanks and interests, no request | FYI | No outstanding request appears in the supplied context. |
| Explicit introduction request | Waiting on them | A person has been asked to make an introduction. |
| Building entry timeout request | Waiting on them | A concrete change has been requested. |
| Human asks for entrance details | Needs reply | The user owes the requested details. |

All cases also require an empty commitments list: none contains an explicit promise
by the user. Inspect the printed classification notes for who owes what; exact
wording is not scored.

## Observations and limits

The reviewed store had 511 threads: 2 needs-reply, 10 waiting-on-them, 372 FYI,
126 noise, and 1 done. The eight-thread convenience sample contained both
needs-reply entries and six waiting-on-them entries. Clear errors included a
calendar invitation, an optional permission notice, and a closing thank-you.
All ten waiting-on-them records lacked triage notes, consistent with the old
automatic outbound transition. This sample does not measure overall accuracy.

The live evaluation was attempted but could not reach the configured Atlas endpoint
at `127.0.0.1:8080`; llama was intentionally stopped while Atlas trains models.
There is therefore no current-model accuracy result. Rerun after the service is available, review the
provisional labels, and expand the cases before drawing quality conclusions.

Existing stored classifications are unchanged. New reminder text displays bounded
participant and triage-note context when available; it cannot recover missing notes
or correct stale classifications by itself.
