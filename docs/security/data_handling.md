# Data handling and audit

This page states what Rungated sees, what it stores, what it sends elsewhere and how far the audit trail can be trusted. It describes the code as shipped. Where a guarantee has a limit, the limit is stated.

## What passes through

Every action the gateway evaluates carries text: prompts, model output, tool arguments, tool results, MCP resources, HTTP bodies seen by the egress proxy. Detectors read that text in memory. Tier 1 models run in process on the gateway host. The optional LLM judge (Tier 2) sends the held action to the model you configure, so enabling it sends that text to that provider. Leave it off where data must not leave your network.

## What is stored

| Data | Where | Content |
| --- | --- | --- |
| Decision events | Store (embedded file or Postgres), event bus | Verdict, score, findings, the action with text. Text is truncated to `MaxStoredText` (default 4000 characters). Spans matched by non injection detectors are replaced with `[masked:<class>]` before storage |
| Audit records | Store | Sequence number, time, event id, tenant, agent, type, verdict, score, a SHA 256 digest of the event, previous hash, own hash. No action text |
| Vault mappings | In memory, per session | Original value to token mapping for redaction and restore. Expires after 24 hours of idle time |
| Approvals | Store | The held action, approver, outcome |
| Forensic snapshots | Local files or S3 | Written on containment. Contain action text and recent session history, so treat them as sensitive and apply bucket level controls |
| Master key | `data_dir/master.key` (mode 0600) or `RUNGATED_MASTER_KEY` | 32 byte key. Per tenant keys are derived from it. Back it up, losing it makes encrypted data unrecoverable |

Per agent control: `spec.privacy.storeContent: false` stores no action text at all, only verdicts, scores and findings. Use it for agents that handle the most sensitive data.

## Limits of masking in stored text

Masking removes values that a detector recognised. A value no detector recognised is stored as is, within the 4000 character cap. Injection findings are deliberately not masked because the matched text is the evidence. If your compliance position is that no customer text may be stored, set `storeContent: false` instead of relying on masking.

## Redaction before the model

With `privacy.redactBeforeModel: true` and matching `redactClasses` or default sensitive classes, matched values are replaced by session scoped tokens before the request reaches the model provider. Tokens in the model response are restored on the way back, including inside streamed output and, when `restoreInToolArgs` is true, inside tool arguments. Values are encrypted with AES GCM under the tenant key while held. The provider never sees the original value, but it does see the surrounding text.

## Where data goes

| Destination | When | What |
| --- | --- | --- |
| Model providers | Always, for allowed LLM requests | Request text after redaction |
| Webhooks, and in Enterprise ClickHouse, Slack, email | When configured | Event payloads. The same masking and truncation as stored events apply. Webhooks are signed with HMAC SHA 256 |
| LLM judge, Enterprise | When enabled | The held action text |
| Hosted models | When `cloud.api_key` is set | The text being scored. Scored in memory and not stored |
| S3, Enterprise | When snapshots are pointed at it | Forensic snapshots |
| Anthropic, any telemetry | Never | The gateway makes no calls except those you configure. There is no phone home |

## Audit trail

Each audit record includes the hash of the previous record, so changing, deleting or reordering a record breaks every later hash. `GET /v1/audit/verify` recomputes the chain, and with events checking on it also compares each record against the stored event digest. The Console has the same check.

What this protects: accidental corruption and tampering with part of the history by someone who can edit rows but does not recompute the whole chain.

What this does not protect: an attacker with write access to the store who rewrites the chain from the point of change onward and recomputes every hash. To close that gap, export the head hash on a schedule to a system the gateway operator cannot write to, such as a write once bucket, a ticket or a separate log service, and compare it during verification. The verify response returns `head_hash` and `records` for this purpose.

## Audit timing

Audit appends are asynchronous. A decision is returned to the caller first and the audit record follows, usually within tens of milliseconds on the embedded store and local bus. If the process is killed with SIGKILL inside that window, the decision was made and enforced but its audit record is lost, because the local bus is in memory. Records already written survive a kill and the chain still verifies, this is tested in `scripts/outage_test.py`. For deployments where no decision may go unaudited, use the NATS JetStream bus in Enterprise, which persists events before they are processed, and keep Postgres as the store.

## Access control

The Console and control API use an admin API key. Enterprise adds scoped keys per role (security, developer, auditor) and tenant scoping on containment and release. Agents authenticate with their own agent key or a short lived agent token. `serve --dev` turns authentication off and is for local use only.

## Retention and deletion

The embedded store keeps everything until you delete the data directory. Postgres retention is yours to configure. The vault holds mappings in memory only and clears them after the session TTL or when the process exits.
