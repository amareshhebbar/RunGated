# Quickstart examples

The same support agent protected three ways: LangGraph, the OpenAI Agents SDK and the Vercel AI SDK. Each example makes four tool calls. Two are allowed, a refund over the 100 dollar limit is blocked, and a tool the rulebook does not list is blocked. No model API key is needed.

```bash
bash examples/quickstart/run.sh
bash examples/quickstart/run.sh python
bash examples/quickstart/run.sh node
```

The script starts a gateway on a free port, registers `agent.yaml`, runs the examples and stops the gateway. Each line prints PASS or FAIL; the script exits non zero on any FAIL.

Guides: `docs/public/guides/`.
