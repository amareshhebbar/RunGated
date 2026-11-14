# LLM proxy

Point any OpenAI or Anthropic client at the gateway. No other code changes are needed.

## Endpoints

| Provider | Base URL |
| --- | --- |
| OpenAI chat completions and responses | `http://GATEWAY:8080/llm/openai/v1` |
| Anthropic messages | `http://GATEWAY:8080/llm/anthropic` |

Streaming and non streaming requests are both supported. The gateway reaches the real provider with the key set in its own environment (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`), so your agents never hold provider keys.

## Identify the agent

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/llm/openai/v1",
    api_key="unused",
    default_headers={"X-Rungated-Agent": "finance-agent", "X-Rungated-Session": "run-42"},
)
```

```python
import anthropic

client = anthropic.Anthropic(
    base_url="http://localhost:8080/llm/anthropic",
    api_key="unused",
    default_headers={"X-Rungated-Agent": "finance-agent", "X-Rungated-Session": "run-42"},
)
```

In production use a short lived token instead of bare headers:

```bash
rungated token --gateway http://localhost:8080 --agent finance-agent --key rgk_agent_key --session run-42
```

Send it as `X-Rungated-Token`. Tokens are revoked when an agent is contained.

## What happens on a request

* Messages, tool results and tool descriptions are scanned.
* Personal data and secrets are replaced with vault tokens.
* Hidden instructions in untrusted content are quarantined.
* The model allow list, rate limits and budgets from the manifest are applied.
* A risky request is held or blocked.

## What happens on a response

* Vault tokens are restored for the user.
* Every tool call the model proposes is checked against the manifest. A blocked call is replaced with a clear message.
* A held call waits for approval.
* Streamed text is watched for secret leaks and image beacon exfiltration.

## Response headers

| Header | Value |
| --- | --- |
| `X-Rungated-Verdict` | The enforced verdict |
| `X-Rungated-Score` | Risk score 0 to 100 |
| `X-Rungated-Action-Id` | Id to find the action in the Console and audit log |

## Errors

A blocked request returns HTTP 403 with `type: rungated_blocked`, the verdict and the reasons. A provider failure returns `rungated_upstream_error`.
