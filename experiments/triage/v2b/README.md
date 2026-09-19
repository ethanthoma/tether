# Contextual v2 training result

The broader corpus adds 400 independently reviewed cases from 100 families,
including 160 two-message exchanges. Both blind reviewers agreed with their
assigned author labels. Combined training contains 1,293 cases; development remains
120 cases. The fresh 120-case, 30-family held-out test was independently reviewed,
but no model predictions have been generated for it.

The fixed recipe selected epoch one. Raw development accuracy rose from 68/120
(56.7%) to 89/120 (74.2%). However, the original global confidence grid still chose
1.0, so accepted coverage remains zero. This candidate fails the release coverage
requirement and has not been enabled. `training-report.json` preserves its full
training history. The previous rejected model remains a read-only production
observer while development-only diagnosis continues.

`training.json` combines the original approved v2 train/dev export and the reviewed
addition. `test.json` comes from `reviews/v2b-test/reconciled/reviewed.json`.
Family, exact-message, and trigram separation checks pass for all three partitions;
the maximum cross-partition trigram Jaccard score is 0.222223. Review archives
preserve packets, author mappings, evidence, and self-attested independence.

The release evaluator binds model identity, training input/report hashes, fixed
recipe metadata, checkpoint selection, partition separation, test hash, and the
referenced frozen plan. Metadata consistency does not independently prove training
chronology. The plan was committed as `0598d7e` before fitting this candidate.
All corpora are synthetic; neither model-context label agreement nor development
accuracy establishes real-mail performance.
