# Triage classifier evaluation

`cases.json` is a 22-case development challenge set: six anonymized paraphrases
from the earlier mailbox review and sixteen synthetic cases. It contains no
original addresses, identifiers, links, or message bodies. Each case records
chronological messages, direction, expected label, rationale, and provenance.
Labels are **provisional assistant judgments**, not human-approved ground truth.
This set is too small and development-driven to estimate production accuracy or
train a useful model. Keep it out of training if later used for evaluation.

## Labels and review

- `needs_reply`: the user owes a reply or concrete personal deadline action.
- `waiting_on_them`: another person owes the user an answer or action.
- `fyi`: information or a resolved exchange with no remaining obligation.
- `noise`: bulk promotions/newsletters without a personal obligation.
- `abstain`: missing context, unclear ownership, or material obligations both ways.

Review the rationale as well as the label before using examples for training.
Contrastive cases reverse message direction; others cover closing thanks versus
unresolved requests, quoted questions, calendar notices, automated deadlines,
missing attachments, and mixed obligations. This benchmark evaluates direction
classification only. Commitment extraction needs a separate evaluation.

## Run without any model

```sh
nix-shell -p go --run 'go test -mod=vendor -run "^TestTriageOfflineBaseline$" -count=1 -v .'
```

The baseline is a small hand-written lexical classifier, **not a trained model**.
It looks at the latest message, strips common quoted tails, and abstains when no
rule applies. It deliberately provides a simple reference for whether a learned
model and thread context add value. It does not produce calibrated confidence.
No production state, network, LLM, or notifications are involved.

## Compare a purpose-trained classifier

Export one prediction per case as a JSON object, for example
`{"resolved_shipping":"fyi", "calendar_invitation":"abstain", ...}`.
Use actual case IDs for every entry; ellipses are illustrative, not valid input.
Allowed values are the five labels above. Feed only `messages` to the classifier;
exclude IDs, expected labels, rationales, and provenance to prevent label leakage.

```sh
TETHER_TRIAGE_PREDICTIONS=/private/predictions.json \
  nix-shell -p go --run 'go test -mod=vendor -run "^TestTriageImportedPredictions$" -count=1 -v .'
```

## Optional small-language-model comparison

Use an explicitly configured endpoint when one is available; none is started or
contacted by the offline benchmark. The request contains only anonymous messages.
The endpoint selects the model; record its model/version and quantization alongside
results. The fixed experimental prompt is in `triage_eval_test.go`, temperature is
zero, and the output budget is 512 tokens. It supports abstention and does not
change the production triage prompt or pipeline.

```sh
TETHER_LLM_URL=http://127.0.0.1:8080 \
  nix-shell -p go --run 'go test -mod=vendor -tags triage_eval -run "^TestTriageEmailExamples$" -count=1 -v -timeout 10m .'
```

Supply `LLAMA_API_KEY` if required. Model/transport errors and invalid labels fail
the evaluation; they are not silently scored as abstentions. No local-model result
is available while llama is intentionally stopped.

## Interpret results

Both runners emit identical counts and an expected-by-predicted confusion matrix:

- Coverage = `accepted / cases`; selective accuracy = `correct_accepted / accepted`
  (undefined if none accepted).
- `false_reminders`: an actionable prediction with the wrong obligation/owner,
  including ambiguous cases confidently assigned an obligation.
- `false_silencing`: a genuinely actionable example classified FYI or noise.
- `actionable_abstained`: work left for review, rather than automatically silenced.
- `ambiguous_accepted`: cases labeled abstain that the classifier decided anyway.
- `correct_abstentions`: intentionally ambiguous cases correctly left undecided.

Evaluation prints mistakes rather than asserting perfect model accuracy. Passing
Go tests validates the harness, not model readiness. An all-abstaining classifier
has zero false reminders but zero coverage; compare both. No automatic promotion
threshold is configured. Before deployment, human-review more labels, collect a
separate evaluation set grouped by thread/source to avoid leakage, and choose
confidence thresholds on development data. Production classifications are unchanged.
