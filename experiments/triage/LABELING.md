# Synthetic triage labeling specification

Version: 1. Scope: the offline five-label obligation classifier.

## What the reviewer sees

Read the supplied one or two messages in chronological order. `outbound: true`
means the user sent the message; `false` means another sender did. Judge only the
visible exchange. Do not invent earlier requests, missing attachments, deadlines,
or an obligation to acknowledge every message. Quoted material is history, not a
new request. The latest message changes only the obligations it actually addresses.

## Labels

| Label | Meaning in this experiment |
| --- | --- |
| `needs_reply` | A clear, outstanding reply or concrete action belongs to the user. This includes an explicit unfinished promise made by the user. |
| `waiting_on_them` | A clear, outstanding reply or concrete action is owed to the user by another person. |
| `fyi` | Information worth seeing, with no remaining obligation supported by the visible exchange. |
| `noise` | Bulk promotional/newsletter content with no personal obligation. Automation alone does not establish this label. |
| `abstain` | Ownership or context is insufficient, or material unfinished obligations belong to both sides. |

These are the existing experimental meanings, not a new production policy.
Production's `needs_reply` normally requires a human-requested prose reply (with
a personal-deadline exception), and production extracts user promises separately.
Flag `policy_mismatch` when a case depends on that difference; keep the
experimental label but withhold the case from the reviewed export. Resolving this
boundary requires an explicit policy decision, not quietly changing production.

## Resolve outstanding obligations

- **Cancellation or replacement:** disregard a request explicitly withdrawn,
  fulfilled elsewhere, or superseded. Retain unrelated unfinished obligations.
  “We found another driver; forget the pickup” closes the pickup request.
- **Partial completion:** completing one part does not complete a remaining
  requested part. “The figures are attached; I'll send the notes tomorrow” leaves
  the sender's promised notes outstanding.
- **Promises:** count a specific commitment (“I'll send the draft”), not a wish
  (“I hope to finish”) or speculation (“perhaps Sam can help”). A user-only promise
  receives `needs_reply` under the experimental convention and `policy_mismatch`.
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
- **Direction:** flipping the sender changes who owes an action, not whether
  that action exists. Flag implausible reversed notices or broken role references.

## Review record

For every case, supply one label, at least one evidence span, a short rationale,
and zero or more flags. Evidence has a zero-based `message_index` and a nonempty
`quote` copied exactly from that message. Evidence supports the interpretation;
its presence alone does not establish correctness.

Allowed flags:

- `policy_mismatch`: experimental and production meanings differ.
- `insufficient_context`: the visible exchange cannot support an unambiguous owner.
- `unnatural_direction`: message direction or participant references are implausible.
- `label_ambiguity`: multiple interpretations remain plausible under these rules.

A reasoned `abstain` is a valid label. Flags separately request adjudication and
withhold the family even when author and reviewer agree on that label. Review
messages without viewing author labels, model predictions, scenario IDs, or split
metadata. Identify yourself and attest that author labels were unseen. This is
self-reported independence, not proof of human review or an external quality audit.
