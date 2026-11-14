# Rungated

Runtime security gateway for AI agents. Every model call, tool call and outbound request your agent makes goes through Rungated. It checks the action against the rules you wrote for that agent, scores the risk, and then allows, warns, redacts, holds for a human, blocks or contains it.

One Go binary. No accounts, no external services, Apache 2.0.

```bash
curl -fsSL https://github.com/amareshhebbar/rungated/releases/latest/download/install.sh | sh
rungated serve --dev
```

Open http://localhost:8080 for the Console.

## What it catches

* Prompt injection hidden in web pages, emails, documents, tool results and MCP tool descriptions
* Secrets and PII leaving through model calls, tool arguments, URLs and image links
* Tools and hosts the agent was never meant to touch
* MCP tools that change after you approved them
* Runaway loops, cost spikes and rate abuse

Every decision lands in a hash chained audit log you can verify.

## See it work

```bash
make demo
```

Runs a LangGraph finance agent against a poisoned MCP server and an evil web page, once without Rungated and once through it, and prints the scoreboard.

## Connect your agent

Point your model client at the gateway:

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/llm/openai/v1",
    api_key="unused",
    default_headers={"X-Rungated-Agent": "finance-agent", "X-Rungated-Session": "run-1"},
)
```

Anthropic works the same way at `/llm/anthropic`. MCP servers go through `/mcp/{server}`, and any HTTP traffic through the egress proxy on port 8081. Python and TypeScript SDKs live in `sdk/`.

Write the agent rulebook by hand with `rungated init --id my-agent`, or let Rungated learn it from real traffic.

## Models

Rules catch the known patterns. Models catch the paraphrased, translated and new ones.

| Model | Kind | Access |
| --- | --- | --- |
| `rungated-embed-mini` | embedder | free forever, download or hosted |
| `rungated-guard-mini` | injection classifier | free forever, download or hosted |
| `rungated-embed-multi` | multilingual embedder | 30 day free trial, hosted |
| `rungated-guard-small` | multilingual classifier | 30 day free trial, hosted |
| `rungated-pii-small` | PII span NER | 30 day free trial, hosted |
| `rungated-guard-large` | high recall classifier | 14 day free trial, hosted |
| `rungated-pii-multi` | multilingual PII NER | 14 day free trial, hosted |
| `rungated-judge` | LLM judge | 14 day free trial, hosted |

```bash
rungated models list
rungated models pull rungated-embed-mini
```

Free models run on your machine with the ONNX build. Hosted models need a free API key and nothing else. When a trial ends the gateway falls back to the free models on its own. You can also load your own ONNX classifier, NER or embedder.

## Free and Enterprise

Rungated Free is the full gateway. Enterprise adds what a security team needs to run it across many agents and teams.

| | Free | Enterprise |
| --- | --- | --- |
| LLM, MCP and egress proxies | yes | yes |
| Rules, Cedar policy, risk scoring, all six verdicts | yes | yes |
| Rule and model detection, your own models | yes | yes |
| Console, approvals in the Console, containment | yes | yes |
| Embedded store, Postgres, Helm, Docker | yes | yes |
| Hash chained audit log, webhooks | yes | yes |
| Free models | yes | yes |
| Trial models | 14 or 30 days | always on |
| Roles and tenant scoped keys | | yes |
| Slack and email approvals | | yes |
| LLM judge | | yes |
| NATS, ClickHouse and S3 snapshots | | yes |
| Air gapped bundle | | yes |
| Support and SLA | | yes |

Enterprise and model keys: hebbar.gvamaresh@gmail.com

## Docs

Read them at https://rungated.readthedocs.io: quickstart, guides for LangGraph, OpenAI Agents and the Vercel AI SDK, models, configuration, deployment and the full API. The source is in [`docs/`](docs).

## What it does not do

Rungated does not stop prompt injection in general, and it cannot protect an agent that skips the gateway. It enforces the rules you declare and detects known attack patterns. See `docs/security/limits.md`.

## License

Apache 2.0. The model weights are under the [Rungated Model License](MODEL_LICENSE.md).
