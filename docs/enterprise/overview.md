---
description: Roles, Slack approvals, the LLM judge, HA event bus, analytics, air gapped installs and support.
---

# Enterprise

Enterprise is the same gateway with the features a security team needs to run it across many agents, teams and environments. See the full comparison on [Free and Enterprise](../editions.md).

<div class="grid cards" markdown>

*   :fontawesome-brands-slack:{ .lg } **Slack and email approvals**

    Approve held actions from Slack, limited to named people.

    [:material-arrow-right: Open](slack.md)

*   :material-lock-outline:{ .lg } **Air gapped install**

    One signed offline bundle with the models included.

    [:material-arrow-right: Open](airgap.md)

*   :material-account-group-outline:{ .lg } **Roles and tenants**

    Admin, security, developer and auditor keys, each limited to a tenant if you want.

*   :material-chart-line:{ .lg } **Scale and analytics**

    NATS JetStream so no decision goes unaudited, ClickHouse for analytics, S3 snapshots.

</div>

## Roles

| Role | Can |
| --- | --- |
| `admin` | everything |
| `security` | everything except deleting agents and other admin actions |
| `developer` | read, register agents and ask for decisions |
| `auditor` | read and verify the audit log |

```yaml
auth:
  api_keys:
    - {name: platform, key: "...", role: admin}
    - {name: soc, key: "...", role: security, tenant: payments}
    - {name: compliance, key: "...", role: auditor}
```

## LLM judge

When the score lands in the hold band, the judge reads the action and the agent's purpose and can release a clearly safe action or keep it held. Point it at any OpenAI compatible endpoint, or at `rungated-judge` on the hosted API.

```yaml
judge:
  base_url: https://api.rungated.dev/v1
  api_key: rk_your_key
  model: rungated-judge
```

Turn it on per agent with `risk.judgeOnHold: true` in the rulebook.

## Event bus and sinks

```yaml
bus:
  kind: nats
  url: nats://nats:4222
clickhouse:
  url: http://clickhouse:8123
snapshots:
  kind: s3
  endpoint: https://s3.amazonaws.com
  bucket: rungated-forensics
```

## Talk to us

Email hebbar.gvamaresh@gmail.com for a pilot.
