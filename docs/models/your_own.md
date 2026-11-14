---
description: Load any ONNX classifier, token NER, GLiNER or embedder.
---

# Use your own model

Any model exported to ONNX with a Hugging Face `tokenizer.json` works. You need the ONNX build of the gateway, see [Run models locally](local.md).

## Classifier

A sequence classifier with `input_ids` and `attention_mask` inputs and `logits` output.

```yaml
tier1:
  enabled: true
  classifier_path: /models/my-guard/model.onnx
  tokenizer_path: /models/my-guard/tokenizer.json
  injection_index: 1
  threshold: 0.5
```

`injection_index` is the logit that means attack. Leave it out for a single logit model.

## Named entity recognition

A token classification model with `labels.json` next to it, or a GLiNER export. The gateway detects GLiNER by its `span_idx` input.

```yaml
tier1:
  enabled: true
  ner_path: /models/my-ner/model.onnx
  ner_labels_path: /models/my-ner/labels.json
```

## Embedder

Any sentence embedding model. The output can be `sentence_embedding`, or `last_hidden_state`, which the gateway mean pools.

```yaml
embed:
  enabled: true
  model_path: /models/my-embedder/model.onnx
  bank_path: /models/my-embedder/bank.jsonl
```

## Check speed before you ship

```bash
rungated tier1 bench --classifier /models/my-guard/model.onnx --iters 200
```

Keep p99 under the Tier 1 timeout (`tier1.timeout_ms`, default 40).
