# Synthetic triage data and description length

[FINETUNING.md](FINETUNING.md) records the subsequent supervised encoder experiment
on these unchanged splits, including its confidence/coverage limitation.

This dataset is fully synthetic: no mailbox exports, public email corpora, or copied
benchmark messages. The task definition is informed by earlier development work.
`synthetic_scenarios.json` contains 72 authored scenario families, each with three
contrasting versions. Applicable direction reversals produce 387 examples:

| Split | Families | Examples |
| --- | ---: | ---: |
| Train | 48 | 261 |
| Development | 12 | 63 |
| Test | 12 | 63 |

All splits cover six domains and twelve behaviors: direct/indirect requests,
promises, follow-ups, resolution, mixed ownership, missing context, quoted history,
automated notices, bulk versus personal mail, negation, and informal formatting.
Scenario variants and reversals remain together. Notices and bulk-mail families
are not mechanically reversed into implausible user-sent notices.

## Research translated into checks

- [Li et al., 2024](https://arxiv.org/html/2407.12813v1): topic variation and iterative
  evaluation motivate distinct authored situations, rather than scaling one template.
  More synthetic rows do not guarantee better downstream performance.
- [CheckList, 2020](https://aclanthology.org/2020.acl-main.442/): capability coverage,
  contrasting obligation states, and direction invariants guide the test matrix.
- [Simula, 2026](https://arxiv.org/html/2603.29791v1): factor-based coverage and explicit
  generation requirements motivate domain/capability annotations and evidence spans.
  This implementation does not reproduce Simula's independent critic pipeline.
- [Li et al., 2023](https://aclanthology.org/2023.emnlp-main.647/): subjectivity can weaken
  synthetic supervision. Ambiguous or mixed ownership receives `abstain` instead
  of an invented obligation.

Each version declares an obligation state relative to the latest sender:
`sender`, `recipient`, `both`, `none`, `unknown`, or `bulk`. The exporter derives
the label from that state and message direction. Evidence must occur verbatim in
a visible message. State annotations are supplied by the author; this is not a
semantic correctness oracle. Labels remain **single-author, self-reviewed,
provisional**, without independent model or human validation.

The exporter rejects duplicate threads, families or normalized messages crossing
splits, missing evidence, and cross-split word-trigram Jaccard similarity >=0.65.
That lexical threshold is a heuristic, not a semantic leakage guarantee. The
observed maximum is 0.1429. Regression tests also reject copied messages from the
old seed/challenge sets. Neither set is used in this training run.

## Kolmogorov complexity and MDL

Kolmogorov complexity describes the shortest program producing data; exact values
are uncomputable. Practical MDL restricts the model/coding scheme. See
[Vitanyi and Li](https://arxiv.org/abs/cs/9901014) and
[Blier and Ollivier](https://arxiv.org/abs/1802.07044).

For classification, the useful target is the description of labels given messages,
`L(Y | X, encoder, protocol)`. Maximizing compressed text length would reward random
noise. Minimizing label codelength alone could instead reward easy, repetitive
examples. We therefore report it alongside behavioral coverage and actual errors;
it is not a dataset acceptance score or a ranking of intrinsic reasoning difficulty.

`mdl.py` implements a conditional prequential code. It shuffles **training families**
with seeds 41/42/43, encodes the first eight families uniformly, and fits the existing
classifier on previous families before encoding the next block. Cumulative family
boundaries are 8, 16, 32, and all available training families. The encoder, inputs,
family boundaries, order, and learning algorithm are shared side information;
encoder weights are not charged. This cannot compare total model sizes.
The diagnostic always loads the pinned public encoder from the local cache.
Passing a fine-tuned encoder is deliberately unsupported: it would already know
labels from later families, invalidating the prequential measurement.

For blocks without two previously seen classes, use the Laplace-smoothed prior.
Otherwise add 1e-6 probability mass to every class and renormalize, including
previously unseen labels. Compare against an online Laplace class-frequency code
and a fixed shuffled-label control (seed 2026). Development/test labels never enter
this diagnostic. The three orderings are sensitivity checks, not independent trials.

Mean bits per training label:

| Code | Bits |
| --- | ---: |
| Uniform five-label code | 2.322 |
| Previous-label frequency prior | 2.162 |
| MiniLM plus learned head | 1.950 |
| Same learner, shuffled labels | 2.345 |

The representation captures some predictable structure. This does not validate
the labels, prove useful semantic novelty, or establish real-mail generalization.

## Train and reproduce

Use the environment and dependency installation in [TRAINING.md](TRAINING.md).
From the repository root, choose fresh output names:

```sh
nix-shell experiments/triage/shell.nix
experiments/triage/.venv/bin/python experiments/triage/build_synthetic.py \
  --output experiments/triage/runs/my-synthetic
HF_HUB_OFFLINE=1 experiments/triage/.venv/bin/python experiments/triage/train.py train \
  --data experiments/triage/runs/my-synthetic/data.json \
  --output experiments/triage/runs/my-synthetic-model
HF_HUB_OFFLINE=1 experiments/triage/.venv/bin/python experiments/triage/mdl.py \
  --data experiments/triage/runs/my-synthetic/data.json \
  --output experiments/triage/runs/my-synthetic/mdl.json
HF_HUB_OFFLINE=1 experiments/triage/.venv/bin/python experiments/triage/train.py predict \
  --model experiments/triage/runs/my-synthetic-model \
  --data experiments/triage/runs/my-synthetic/holdout.json \
  --output experiments/triage/runs/my-synthetic/predictions.json
experiments/triage/.venv/bin/python -m unittest discover \
  -s experiments/triage -p 'test_*.py' -v
```

Offline training requires the pinned public encoder already cached by the pilot;
omit `HF_HUB_OFFLINE=1` once to download it if needed. Generation itself uses no
network, inference endpoint, or external API. Source text and decisions are fully
reviewable. The completed run uses `runs/synthetic-v1/` and
`runs/minilm-synthetic-v1/`. Weights and generated splits remain ignored; source
scenarios, code, and [synthetic-report.json](synthetic-report.json) are committed.

## Test result and next iteration

Both models were evaluated on the same 63 new synthetic test cases:

| Model / cutoff | Accepted | Correct accepted | False reminders | False silencing |
| --- | ---: | ---: | ---: | ---: |
| Original pilot / none | 50 | 20 | 22 | 6 |
| New synthetic training / none | 50 | 26 | 10 | 11 |
| Original pilot / 0.65 | 12 | 6 | 6 | 0 |
| New synthetic training / 0.85 | 1 | 1 | 0 | 0 |

The new cutoff was selected on development data before testing. It leaves all 29
actionable test cases undecided. Raw accuracy improves but false silencing worsens;
the conservative model has inadequate coverage. No promotion or threshold retuning.

This is a controlled seed corpus, not a validated high-quality production dataset.
There are only two noise test examples; paired examples are correlated; language
is English and messages are short. The same author designed every split, and many
examples explicitly state completion or uncertainty. Those can become shortcuts.
The next fully synthetic iteration should independently review labels, add less
explicit and longer-context scenarios, and compare encoder fine-tuning against this
frozen baseline. Use training-only diagnostics to guide expansion and reserve new
scenario families for future evaluation; do not optimize against these test errors.
