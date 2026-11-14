# rungated

Python SDK for the Rungated runtime AI agent firewall.

```python
from rungated import Rungated, protect

ab = Rungated(gateway="http://127.0.0.1:8080", agent_id="finance-agent", agent_key="...")

client = ab.openai()
reply = client.chat.completions.create(model="gpt-4o", messages=[{"role": "user", "content": "hi"}])

@protect(ab, name="get_invoice", server="finance-mcp")
def get_invoice(invoice_id: str) -> dict: ...
```

Every function has an async twin prefixed with `a` (`adecide`, `acheck_tool`, `acheck_result`, `ascan`, `arestore`).

Adapters live in `rungated.integrations`: `langgraph` (`RungatedToolNode`, `protect_tools`) and `openai_agents` (`protect_function_tools`, `use_gateway`).

Environment variables: RUNGATED_GATEWAY, RUNGATED_AGENT, RUNGATED_AGENT_KEY, RUNGATED_TOKEN, RUNGATED_API_KEY, RUNGATED_SESSION.
