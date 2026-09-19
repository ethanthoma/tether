# V2f development result

Rejected before held-out evaluation. The fixed recipe selected epoch one on
1,989 training and 152 fresh development cases. Raw development accuracy was
88/152 (57.9%); safe cutoffs accepted 26/152 (17.1%). Both actionable classes
retained support, but coverage and generalization were insufficient to justify
consuming another test. This is a development decision, not a failed held-out
gate or a claim that test coverage is known.

All 154 cases in `test.json` remain unused for predictions. The complete training
history is in `training-report.json`. Later epochs fit the training set closely
while development cross-entropy worsened. New wording was varied, but all 300
training additions used outgoing-to-incoming exchanges; evaluation includes
other direction patterns. These are limitations, not grounds to weaken the gate.

Production classifier authority remains disabled. V2g records an encoder change
using the same reviewed partitions and the still-unused test.
