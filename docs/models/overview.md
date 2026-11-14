---
description: Free models forever, stronger models free to try, or bring your own.
---

# Models

Rules catch known attack patterns in microseconds. Models catch what rules miss: paraphrased injections, other languages and new phrasing. Rungated uses three kinds of model.

* **Embedders** turn text into vectors. Rungated compares each text with a bank of known attacks and safe examples and scores how close it is to an attack.
* **Classifiers** read a text and give the probability that it is an injection or jailbreak.
* **Span NER** finds names, addresses, IDs and other personal data so they can be redacted.

## The catalog

| Model | Kind | Base | Access |
| --- | --- | --- | --- |
| `rungated-embed-mini` | embedder | all MiniLM L6 v2, 22M | free forever |
| `rungated-guard-mini` | classifier | DeBERTa v3 xsmall, 70M | free forever |
| `rungated-embed-multi` | embedder, 100 languages | multilingual E5 small, 118M | 30 day trial |
| `rungated-guard-small` | classifier, multilingual | mDeBERTa v3 base, 280M | 30 day trial |
| `rungated-pii-small` | span NER | GLiNER small v2.1, 166M | 30 day trial |
| `rungated-guard-large` | classifier, long context | ModernBERT large, 395M | 14 day trial |
| `rungated-pii-multi` | span NER, six languages | GLiNER multi PII v1, 209M | 14 day trial |
| `rungated-judge` | LLM judge | Qwen2.5 1.5B Instruct | 14 day trial, Enterprise |

```bash
rungated models list
```

## Three ways to run them

<div class="grid cards" markdown>

*   :material-laptop:{ .lg } **On your machine**

    Free models, downloaded once, nothing leaves your network.

    [:material-arrow-right: Open](local.md)

*   :material-cloud-outline:{ .lg } **Hosted**

    Every model, no ONNX install. Trials start on first use.

    [:material-arrow-right: Open](hosted.md)

*   :material-package-variant-closed:{ .lg } **Your own**

    Any ONNX classifier, NER or embedder you trained.

    [:material-arrow-right: Open](your_own.md)

</div>

## When a trial ends

The hosted API answers that the trial is over and the gateway switches to your local models on its own. Nothing breaks and no config change is needed. If you have no local model configured, the rules keep working and the model findings stop.

## Licence

The models are free to run, including commercially. They may not be fine tuned, redistributed or offered as a hosted service. See the [Rungated Model License](https://github.com/amareshhebbar/rungated/blob/main/MODEL_LICENSE.md). Each base model keeps its own Apache 2.0 or MIT licence.
