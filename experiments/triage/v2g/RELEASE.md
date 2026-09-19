# V2g language-inference encoder release plan

Frozen before fitting or held-out predictions. V2f was rejected on development
evidence (17.1% selective coverage, 57.9% raw accuracy). Its 154-case test remains
unused. Further repetitions of the same MiniLM recipe are not sufficient evidence
of progress.

## Encoder intervention

Use [cross-encoder/nli-deberta-v3-xsmall](https://huggingface.co/cross-encoder/nli-deberta-v3-xsmall)
at revision `a150876415327c80daeff35ca6f68f5ed8cf5c24`. Its model card records
Apache-2.0 licensing and SNLI/MultiNLI pretraining. The model has approximately
70.8 million total parameters, including its large vocabulary embedding table.
This is a hypothesis about transfer to conversational obligations, not evidence
of triage accuracy.

Load only pinned safe weights and tokenizer/configuration assets, using built-in
Transformers classes with remote code disabled. Discard the original NLI output
head and construct explicit mean pooling over its 384-dimensional encoder.
Keep normalized joint-thread features, four direction/presence bits, schema 3,
and the combined 512-token limit. No truncation or general-purpose LLM is added.

## Fixed data and recipe

Use `v2f/training.json` unchanged: 1,989 train and 152 development cases. Reserve
the unchanged `v2f/test.json` (154 cases, 77 families) for one evaluation.
All custom fitting data is reviewed synthetic text; no private mail is used.
Preserve the recorded overlap exclusions and historical-evaluation audit.

Keep seed 42, four CPU threads, 12 epochs, batch size 16, logistic head
initialization, inverse-frequency weighted loss, encoder learning rate 2e-5,
head learning rate 1e-3, AdamW weight decay 0.01, and gradient clip 1. Select
the first minimum development cross-entropy checkpoint, including epoch zero.
Keep the existing per-class cutoff grid and zero-error development selection.

Before consuming test, require at least 25% selective development coverage and
correct accepted support for both actionable classes. This additional readiness
check is fixed before fitting. Do not select another checkpoint or tune cutoffs
after reading test results.

## Acceptance and rollout

The held-out gate stays unchanged: all five labels, at least 80 cases and 20
families, zero accepted errors, at least 25% coverage, and a correct accepted
prediction in each actionable class. A failed held-out evaluation consumes this
test and leaves authority disabled. Record all data, artifact, plan, and report
identities and the limitations of correlated synthetic/model-reviewed examples.

Measure CPU inference before promotion: the exact production wrapper must fit
the existing 30-second batch deadline and 2-GiB memory limit. After quality passes,
verify the immutable artifact through the isolated native activation/failure/
rollback gate, then live read-only shadow. Activate only its exact hash and retain
rollback. Preserve all three Bend switches and leave llama off; send no test
notifications.
