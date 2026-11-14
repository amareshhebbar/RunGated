---
description: Rungated Free is the whole gateway. Enterprise adds what a security team needs across many agents.
---

# Free and Enterprise

Rungated Free is open source under Apache 2.0 and has no limits on agents, requests or users of the gateway. Enterprise is for teams that run many agents and need to prove control over them.

| | Free | Enterprise |
| --- | --- | --- |
| LLM proxy for OpenAI and Anthropic | :material-check: | :material-check: |
| MCP proxy with tool pinning | :material-check: | :material-check: |
| Egress proxy | :material-check: | :material-check: |
| Agent rulebooks, Cedar policy, risk scoring | :material-check: | :material-check: |
| Rule detection for secrets, PII, injection, exfiltration | :material-check: | :material-check: |
| Your own ONNX models | :material-check: | :material-check: |
| Free models | :material-check: | :material-check: |
| Trial models | 30 or 14 days | always on |
| Console, approvals in the Console | :material-check: | :material-check: |
| Automatic and manual containment | :material-check: | :material-check: |
| Embedded store, Postgres | :material-check: | :material-check: |
| Docker image, Helm chart | :material-check: | :material-check: |
| Hash chained audit log, signed webhooks | :material-check: | :material-check: |
| Roles and tenant scoped API keys | | :material-check: |
| Slack and email approvals | | :material-check: |
| LLM judge for held actions | | :material-check: |
| NATS JetStream event bus | | :material-check: |
| ClickHouse event sink | | :material-check: |
| Forensic snapshots to S3 | | :material-check: |
| Air gapped bundle | | :material-check: |
| Support and SLA | | :material-check: |

## How the Free build treats Enterprise settings

If a config file asks for an Enterprise feature, the Free build logs one warning and carries on without it. The NATS bus falls back to the local bus, S3 snapshots fall back to local files, and Slack, email and the judge stay off.

API keys with a role other than `admin`, or with a tenant, stop the Free build at startup. That keeps a config written for Enterprise from silently giving every key admin rights.

```bash
rungated edition
```

prints `free` or `enterprise`.

## Get Enterprise

Email hebbar.gvamaresh@gmail.com for a pilot.
