# LangGraph in 10 minutes

Protect every tool call a LangGraph agent makes. When you finish, allowed calls run, and calls outside the agent's rulebook come back to the model as a refusal it can recover from.

## 1. Start the gateway

```bash
docker run --rm -p 8080:8080 -e RUNGATED_API_KEY=change-me ghcr.io/amareshhebbar/rungated:latest serve
```

## 2. Describe your agent

Save as `agent.yaml`. List the tools the agent may use and the limits on their arguments.

```yaml
apiVersion: rungated.dev/v0
kind: Agent
metadata:
  id: support-agent
  name: Support Agent
  owner: you@example.com
  purpose: Looks up orders and issues small refunds
  tenant: default
  version: "1.0.0"
spec:
  model:
    providers: [openai, anthropic]
    allowed: ["gpt-4o*", "claude-*"]
  riskTier: medium
  autonomy:
    default: observe
  tools:
    - name: lookup_order
      server: orders
      description: Fetch one order by id
      args:
        order_id: {type: string, required: true, pattern: "^ORD-[0-9]{5}$"}
    - name: issue_refund
      server: orders
      description: Refund up to 100 dollars
      blastRadius: high
      args:
        order_id: {type: string, required: true, pattern: "^ORD-[0-9]{5}$"}
        amount: {type: number, required: true, min: 1, max: 100}
```

`observe` logs decisions without blocking. Switch to `auto_contain` when you trust the rulebook.

Register it and keep the agent key it returns:

```bash
curl -s -X POST localhost:8080/v1/agents \
  -H "Authorization: Bearer change-me" -H "Content-Type: application/yaml" \
  --data-binary @agent.yaml
```

## 3. Install the SDK

```bash
pip install rungated langgraph langchain-core
export RUNGATED_GATEWAY=http://localhost:8080
export RUNGATED_AGENT_KEY=<agent key from step 2>
```

## 4. Swap `ToolNode` for `RungatedToolNode`

```python
from rungated import Rungated
from rungated.integrations.langgraph import RungatedToolNode

rg = Rungated(agent_id="support-agent")
tools = [lookup_order, issue_refund]
tool_node = RungatedToolNode(rg, tools, server="orders")
```

Use `tool_node` wherever your graph used `ToolNode(tools)`. Nothing else changes.

To also check every model call, point the model client at the gateway:

```python
from langchain_openai import ChatOpenAI

llm = ChatOpenAI(model="gpt-4o-mini", api_key="rungated", **rg.llm_kwargs("openai"))
```

Set `OPENAI_API_KEY` on the gateway, not in the agent.

## 5. See it work

Open [http://localhost:8080](http://localhost:8080), enter the admin key under Settings, and open **Live**. Every tool call shows its verdict. Click one and use **Was this decision right?** to tell us when it got it wrong.

## Runnable example

`examples/quickstart/langgraph_example.py` runs four tool calls through a real LangGraph graph: two allowed, a refund over the limit blocked, and an undeclared tool blocked. Run it with:

```bash
bash examples/quickstart/run.sh python
```
