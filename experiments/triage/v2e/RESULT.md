# V2e result

Rejected for production authority. The frozen recipe selected epoch two from
1,689 training and 120 development cases. Development cutoffs accepted 25/120
cases without error and retained both actionable classes.

The single evaluation on the previously unused v2d test accepted 37/120 cases
(30.8%), with two errors: ambiguous cases became false reminders. Raw accuracy
was 62.5%. Coverage passed, but the zero-accepted-error requirement failed.
Full identities, per-class metrics, and predictions are in `release-report.json`;
the complete fitting history is in `training-report.json`.

No authority switch or production model changed. The v2d test is now consumed
and must not approve later candidates. A read-only training/development review
also found repeated wording across newly added families and a remaining
direction/context imbalance. V2f records the next data intervention separately.
