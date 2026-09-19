# Fresh evaluation candidate authoring

`evaluation-candidate.json` contains 240 newly authored, fully synthetic cases:
30 development families and 30 test families, with four cases in each family.
The author used `LABELING.md` and `POLICY.md` version 2 and inspected the existing
v2 training family inventory to avoid reusing its scenarios. The author did not
consult model predictions or use existing evaluation labels to assign labels.
Old datasets were accessed programmatically only for separation checks after
authoring. Each case includes an explicit rationale and exact message evidence.

The cases were manually composed, including their semantic differences and
rationales. A temporary serialization script converted those records to JSON;
it did not derive labels from direction, an obligation-state mapper, templates,
or model predictions. The candidate remains pending independent blind review.

| Split | Families | Cases | needs_reply | waiting_on_them | fyi | noise | abstain |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| dev | 30 | 120 | 30 | 26 | 25 | 10 | 29 |
| test | 30 | 120 | 30 | 26 | 25 | 10 | 29 |

Contrasts include outstanding questions, incomplete requested actions, request
cancellation, fulfilled requests, quoted historical questions, acknowledgments,
calendar exceptions, routine notices, explicit automated personal deadlines,
physical tasks, file/photo transfers, user promises, unsolicited incoming
promises, mixed ownership, and intentional uncertainty. Task-routing and
uncertainty abstentions are valid unflagged decisions under the v2 policy.

Validation completed before blind review:

- Unique case IDs; exactly four cases per family; 30 families per split; all five
  labels represented at least ten times per split.
- One or two messages per case; boolean direction; valid nonempty exact evidence
  spans; no author flags.
- Maximum message length 153 UTF-8 bytes and 33 tokens using the cached
  `sentence-transformers/all-MiniLM-L6-v2` tokenizer at revision
  `1110a243fdf4706b3f48f1d95db1a4f5529b4d41`, loaded locally without network.
- `calibrate.check_separation([dev, test, reviewed_v2_train])` passed for partition
  counts `[120, 120, 893]`; maximum cross-partition trigram Jaccard was
  `0.2222222222222222`.
- The same separation audit against old `seed.json`, `cases.json`, the three
  `calibration-{temperature,selection,audit}.json` sources, and
  `runs/synthetic-v1/{data,holdout}.json` passed for `[120, 120, 594]`; maximum
  similarity was `0.2222222222222222`.
- A further audit against `runs/scaling-v1/holdout.json` and
  `runs/scale-source-check/{data,holdout}.json` passed for `[120, 120, 1152]`;
  maximum similarity was `0.14285714285714285`.

These checks establish the specified structural and lexical exclusions, not
independent label correctness or production quality. No model was trained or
queried, and nothing was deployed or committed during authoring.
