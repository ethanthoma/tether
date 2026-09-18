# Initial offline results — 2026-09-18

Dataset: `cases.json`, version 1, 22 development cases with provisional labels.
These results describe this challenge set only; rules were written with the cases
visible, so this is neither held-out accuracy nor evidence for production promotion.

| Baseline | Accepted | Correct accepted | Abstained | False reminders | False silencing | Actionable abstained |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Latest-message lexical rules | 13 | 13 | 9 | 0 | 0 | 5 |
| Always abstain (imported predictions) | 0 | 0 | 22 | 0 | 0 | 10 |

Rule coverage is 59.1%; accepted accuracy is 13/13 on these provisional labels.
Both baselines abstain on all four deliberately ambiguous cases. Always-abstain
has undefined accepted accuracy, not 100% accuracy.

The rules leave these actionable cases for review:

- An indirect introduction request.
- An unanswered request followed by thanks for patience.
- An inbound follow-up requiring earlier context.
- An outbound follow-up requiring earlier context.
- An automated notice with a concrete personal deadline.

A learned candidate should improve coverage on these patterns without introducing
false reminders, false silencing, or confident decisions on ambiguous cases.
Do not tune on this small set and then report the same cases as an independent
accuracy measurement. Human label review and a separately collected, thread-grouped
evaluation set must precede production decisions.

## Frozen MiniLM with a trained head — 2026-09-18

Trained locally on 72 synthetic examples (36 direction-paired scenarios), with 38
development examples (19 scenarios). No private mailbox data or challenge examples
were used for training. The base encoder stayed frozen. See [TRAINING.md](TRAINING.md)
for the pinned model, dependencies, representation, and reproduction commands.

Development cutoff selection chose **0.65**, accepting 6/38 with 6 correct and zero
accepted errors. Without the cutoff, development accepted 32/38 with 26 correct,
six false reminders, and zero false silencing. Development results select the cutoff
and must not be presented as an independent accuracy estimate.

| Challenge candidate | Accepted | Correct accepted | Abstained | False reminders | False silencing | Actionable abstained |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| MiniLM head, no confidence cutoff | 19 | 11 | 3 | 5 | 3 | 1 |
| MiniLM head, development cutoff 0.65 | 7 | 6 | 15 | 1 | 0 | 7 |

At the selected cutoff, coverage is 31.8% and selective accuracy is 6/7 (85.7%).
Three of four deliberately ambiguous cases abstain. `mixed_obligations` incorrectly
becomes `waiting_on_them`, causing the false reminder. Both follow-up examples are
correct, but seven actionable cases still abstain. The classifier therefore does
**not** improve on the rules overall and must not be promoted.

Saved/reloaded inference ran with `HF_HUB_OFFLINE=1`; the existing Go imported
prediction scorer independently confirmed the selective counts. Public predictions
are in `pilot-predictions.json`; weights remain ignored in `runs/minilm-pilot/`.
No threshold or training change was made in response to challenge results.
