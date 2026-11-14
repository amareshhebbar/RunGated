# Configuration

Rungated reads a YAML file (`--config`) and then applies environment overrides. Unknown keys are rejected, so typos fail at startup. The gateway also refuses to start without an admin key: set `RUNGATED_API_KEY`, add `auth.api_keys`, or run with `--dev`.

## Minimal

```yaml
listen: ":8080"
egress_listen: ":8081"
data_dir: /data
store:
  kind: embedded
bus:
  kind: local
upstreams:
  openai:
    base_url: https://api.openai.com/v1
auth:
  require_agent_auth: true
```

## Production

```yaml
store:
  kind: postgres
  dsn: postgres://rungated:secret@postgres:5432/rungated?sslmode=require
```

Enterprise adds `bus.kind: nats` so every decision is persisted before it is processed. See [Enterprise](../enterprise/overview.md).

## Models

```yaml
embed:
  enabled: true
  model: rungated-embed-mini
tier1:
  enabled: true
  model: rungated-guard-mini
cloud:
  classifier: rungated-guard-small
  ner: rungated-pii-small
```

Local models need the ONNX build. Hosted models need `RUNGATED_CLOUD_API_KEY`. See [Models](../models/overview.md).

## Environment overrides

| Variable | Sets |
| --- | --- |
| `RUNGATED_LISTEN`, `RUNGATED_EGRESS_LISTEN` | Listen addresses |
| `RUNGATED_DATA_DIR` | Data directory |
| `RUNGATED_STORE`, `RUNGATED_DATABASE_URL` | Store kind and Postgres DSN |
| `RUNGATED_BUS`, `RUNGATED_NATS_URL` | Event bus, NATS is Enterprise |
| `RUNGATED_OPENAI_BASE_URL`, `RUNGATED_ANTHROPIC_BASE_URL` | Upstream providers |
| `RUNGATED_API_KEY` | Admin API key |
| `RUNGATED_REQUIRE_AGENT_AUTH` | Require agent keys or tokens |
| `RUNGATED_UNKNOWN_AGENTS` | Handling of unregistered agents |
| `RUNGATED_JUDGE_BASE_URL`, `RUNGATED_JUDGE_API_KEY`, `RUNGATED_JUDGE_MODEL` | LLM judge for holds, Enterprise |
| `RUNGATED_SLACK_WEBHOOK_URL`, `RUNGATED_SLACK_BOT_TOKEN`, `RUNGATED_SLACK_CHANNEL`, `RUNGATED_SLACK_SIGNING_SECRET`, `RUNGATED_SLACK_ALLOWED_USERS` | Slack approvals, Enterprise, see [Slack approvals](../enterprise/slack.md) |
| `RUNGATED_CONSOLE_URL` | Console link used in notifications |
| `RUNGATED_TIER1_ENABLED`, `RUNGATED_TIER1_CLASSIFIER`, `RUNGATED_TIER1_NER`, `RUNGATED_TIER1_TOKENIZER` | Tier 1 model |
| `RUNGATED_EGRESS_PARENT_PROXY`, `RUNGATED_EGRESS_INTERCEPT_TLS` | Egress proxy |
| `RUNGATED_EMBED_MODEL` | Local embedder, turns `embed` on |
| `RUNGATED_MODELS_DIR` | Where `rungated models pull` saves models |
| `RUNGATED_CLOUD_API_KEY`, `RUNGATED_CLOUD_URL` | Hosted models |
| `RUNGATED_CLOUD_CLASSIFIER`, `RUNGATED_CLOUD_NER`, `RUNGATED_CLOUD_EMBEDDER` | Which hosted models to use |

Provider API keys for upstreams stay on the gateway. Agents never see them.

## Memory

| Setting | Environment | Default | Meaning |
| --- | --- | --- | --- |
| `store.memory_events` | `RUNGATED_MEMORY_EVENTS` | 20000 | Recent events kept in memory for the Console and the API. Older events stay in `events.jsonl` on disk, which rotates at 256 MB keeping one previous file |

The audit log is never rotated, because every record is chained to the one before it. Plan disk for about 450 bytes per decision.
