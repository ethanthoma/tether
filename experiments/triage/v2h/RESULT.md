# V2h held-out result

Rejected. The fixed 12-epoch run selected epoch twelve at temperature
3.7492674358413796. Raw development accuracy was 130/152 (85.5%); development
cutoffs accepted 61/152 (40.1%) with zero errors and both actionable classes.

The unchanged v2f test was then evaluated once. The candidate accepted 73/154
(47.4%), with four false reminders: three ambiguous cases and one informational
case incorrectly assigned an obligation. Raw test accuracy was 124/154 (80.5%).
The zero-accepted-error gate failed. This test is consumed and this artifact
must not receive production authority.

Artifact: `951f1830d3c52e5f899d080776fecd00b5b66ff39c3ca6cb85c8e3731fe00bac`.
See `training-report.json` and `release-report.json` for the complete evidence.

The representation is stronger than preceding candidates, but 152 development
cases did not support reliable actionable cutoffs. The next experiment freezes
these weights and temperature, uses a larger fresh calibration set with an
explicit conservative cutoff rule, and reserves a separately authored holdout.
It cannot inherit approval from these results. Bend remains active; classifier
authority remains absent.
