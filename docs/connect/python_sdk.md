# Python SDK

```bash
pip install rungated
```

Requires Python 3.10 or newer. The package has one dependency, `httpx`.

## Client

```python
from rungated import Rungated, protect

rg = Rungated(
    gateway="http://localhost:8080",
    agent_id="finance-agent",
    agent_key="rgk_agent_key",
)

client = rg.openai()
anthropic_client = rg.anthropic()
```

`rg.openai()` and `rg.anthropic()` return official clients already pointed at the gateway with the agent identity set.

## Guard a tool

```python
@protect(rg, name="get_invoice", server="finance-mcp")
def get_invoice(invoice_id: str) -> dict:
    ...
```

Before the function runs, the call and its arguments are sent to the gateway. After it returns, the result is scanned for injected instructions and secrets. If the verdict is `block`, the function does not run and `ActionBlocked` is raised. Other outcomes raise `ApprovalDenied` and `AgentContained`. To return the refusal text to the model instead of raising, use `on_block="message"`. A `hold` waits for a human decision.

## Check without wrapping

```python
decision = rg.check_tool("send_email", {"to": "x@example.com"}, server="mail")
if not decision.allowed:
    print(decision.verdict, decision.reasons)

decision = rg.scan("text from an untrusted page", source="web")
```

All methods have async versions with an `a` prefix: `adecide`, `acheck_tool`, `ascan`.

## Frameworks

```python
from rungated.integrations.langgraph import RungatedToolNode, protect_tools
from rungated.integrations.openai_agents import protect_function_tools, use_gateway
```

## When the gateway is unreachable

The client fails closed and raises `GatewayError`. Set `fail_open=True` only for low impact agents. A fail open decision carries the reason "gateway unreachable, failing open" so you can alert on it.

## Register an agent from code

```python
rg.register(open("my-agent.yaml").read())
```
