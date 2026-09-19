# Synthetic triage labeling specification

Version: 2. Scope: future offline reviews aligned with the production reply policy.
Existing datasets, models, and the archived v1 review retain their original
meanings. See [POLICY.md](POLICY.md) for explicit adoption requirements.

## What the reviewer sees

Read the supplied one or two messages in chronological order. `outbound: true`
means the user sent the message; `false` means another sender did. Judge only the
visible exchange. Do not invent earlier requests, missing attachments, deadlines,
or an obligation to acknowledge every message. Quoted material is history, not a
new request. The latest message changes only the obligations it actually addresses.

## Labels

| Label | Meaning under version 2 |
| --- | --- |
| `needs_reply` | A human visibly requested an outstanding prose response from the user, or an automated notice explicitly requires the user personally to act by a stated deadline. |
| `waiting_on_them` | A human owes an outstanding response or action to a visible, explicit request from the user. An unsolicited promise alone does not qualify. |
| `fyi` | Information worth seeing, with no remaining obligation supported by the visible exchange. |
| `noise` | Newsletters, promotions, or routine automated mail with no personal deadline or calendar exception; never worth a nudge. |
| `abstain` | Ownership/context is insufficient, material obligations belong to both sides, or task/commitment routing cannot be represented safely by the other labels. |

The five-label classifier cannot extract commitments or represent every task.
A user-only task or promise, or an unsolicited incoming promise without a visible
user request, must not silently become `fyi`: use `abstain`. If an otherwise
classifiable reply also contains a separate user commitment needing extraction,
abstain rather than losing that routing obligation. Abstention is a classifier
decision, not a production thread state; keep the work available to the existing
triage/commitment path. This specification does not change production behavior.

## Resolve outstanding obligations

- **Cancellation or replacement:** disregard a request explicitly withdrawn,
  fulfilled elsewhere, or superseded. Retain unrelated unfinished obligations.
  “We found another driver; forget the pickup” closes the pickup request.
- **Partial completion:** completing one part does not complete a remaining
  requested part. “The figures are attached; I'll send the notes tomorrow” leaves
  the sender's promised notes outstanding.
- **Promises:** count a specific commitment (“I'll send the draft”), not a wish
  (“I hope to finish”) or speculation (“perhaps Sam can help”). A user-only promise
  receives `abstain` and needs the separate commitment extractor. A promise does
  not erase an outstanding requested answer.
- **Two owners:** explicit outstanding promises or requests on both sides produce
  `abstain`, even if one depends on the other. Do not infer a separate obligation
  merely because a request enables later work.
- **Acknowledgments:** “Thanks” does not cancel an earlier unfinished request.
  It is `fyi` only when no visible obligation remains. “Done” without a visible
  referent can require `abstain`.
- **Notices:** calendar invitations and attendance updates are informational;
  don't invent a prose reply. A personal automated deadline is different from
  a generic marketing deadline. Use `abstain` when action or ownership is unclear.
- **Quoted questions:** a resolved or canceled question in quoted history is not
  outstanding again merely because it appears in the message.
- **Reply versus task:** inbound “Which draft should I use?” is `needs_reply`.
  Inbound “Please carry the boxes upstairs” is `abstain`: the task exists, but
  does not request prose. The same task requested outbound can be `waiting_on_them`.
- **Visible questions:** missing historical details do not erase an explicit
  question. “Is that arrangement happening?” inbound requires a reply; outbound
  establishes waiting. The classifier need not know the answer. Use `abstain`
  when the existence or owner of a request is unclear, such as “That arrangement
  again…” without a question or identifiable disposition. Quoted, rhetorical,
  and canceled questions still require context.
- **Direction:** assess each variant independently; reversal is not a mechanical
  label swap under this policy. Flag implausible reversed notices or broken roles.

## Review record

For every case, supply one label, at least one evidence span, a short rationale,
and zero or more flags. Evidence has a zero-based `message_index` and a nonempty
`quote` copied exactly from that message. Evidence supports the interpretation;
its presence alone does not establish correctness.

Allowed flags:

- `policy_mismatch`: an interpretation requires behavior outside this contract
  and no specified abstention resolves it. Task/commitment routing abstentions
  conform to version 2; do not flag every action or promise.
- `insufficient_context`: a defective/incomplete example needs clarification
  before review. Intentional uncertainty that clearly supports `abstain` is not
  itself a defect and does not require this flag.
- `unnatural_direction`: message direction or participant references are implausible.
- `label_ambiguity`: the rules leave competing labels unresolved, rather than
  deliberately requiring an uncertainty abstention.

A reasoned `abstain` is a valid, potentially unflagged label. Flags request adjudication and
withhold the family even when author and reviewer agree on that label. Review
messages without viewing author labels, model predictions, scenario IDs, or split
metadata. Identify yourself and attest that author labels were unseen. This is
self-reported independence, not proof of human review or an external quality audit.
