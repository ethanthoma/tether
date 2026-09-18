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

No small LLM or trained classifier was evaluated, trained, downloaded, or started.
The optional model runner and imported-prediction scorer are ready for comparison.
