---
description: Use every Rungated model through an API key, with no ONNX install.
---

# Hosted models

The hosted API runs every model on our servers. The plain `rungated` binary is enough, no ONNX Runtime needed.

## Access

| Models | Free key |
| --- | --- |
| `rungated-embed-mini`, `rungated-guard-mini` | free forever, 120 requests a minute |
| `rungated-embed-multi`, `rungated-guard-small`, `rungated-pii-small` | 30 days from your first request to that model |
| `rungated-guard-large`, `rungated-pii-multi`, `rungated-judge` | 14 days from your first request to that model |

Each trial starts on its own the first time you call that model, so you can try one model now and another next month. Paid keys have every model always on.

Ask for a free key at hebbar.gvamaresh@gmail.com.

## Turn it on

```bash
export RUNGATED_CLOUD_API_KEY=rk_your_key
```

```yaml
cloud:
  classifier: rungated-guard-small
  ner: rungated-pii-small
  embedder: rungated-embed-multi
```

Leave out any line you do not want. Restart the gateway. The log shows `hosted models enabled`.

## Check your trials

```bash
rungated models status
```

```
plan free

MODEL                  TIER       ACTIVE   DAYS LEFT
rungated-embed-mini    free       true     unlimited
rungated-guard-small   trial30    true     22
rungated-guard-large   trial14    true     14
```

The gateway also logs a warning when a trial has three days or less left.

## What we keep

Nothing from your requests. The API keeps a hash of your key and, for each trial model, the time of your first request. Text is scored in memory and dropped.

## Settings

| Setting | Environment | Default |
| --- | --- | --- |
| `cloud.api_key` | `RUNGATED_CLOUD_API_KEY` | none |
| `cloud.url` | `RUNGATED_CLOUD_URL` | `https://api.rungated.dev` |
| `cloud.classifier` | `RUNGATED_CLOUD_CLASSIFIER` | none |
| `cloud.ner` | `RUNGATED_CLOUD_NER` | none |
| `cloud.embedder` | `RUNGATED_CLOUD_EMBEDDER` | none |
| `cloud.timeout_ms` | | 400 |

If a hosted call is slower than the timeout, the gateway uses your local models for that action instead.
