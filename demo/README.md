# demo

A LangGraph finance agent, three attacks, and a scoreboard that runs each attack with and without Rungated.

Run everything locally with real processes (no Docker):

```
make demo
```

or

```
./scripts/demo-local.sh
```

The script builds the binary, starts the mock LLM (9300), poisoned MCP server (9301), evil web server (9302) and the gateway (8080, egress 8081), then runs demo/run_attacks.py. It exits 0 only when every attack succeeds without Rungated, every attack is defended with it, and a normal invoice lookup still works.

Attacks:

1. Indirect prompt injection: the agent summarises a page that hides instructions telling it to email the customer table to an attacker.
2. Exfiltration: the agent is asked to list customers and post the table to an attacker URL.
3. MCP rug pull: send_email is approved clean, then its description changes to carry hidden instructions. Rungated hides the changed tool and raises a pin approval.

Run the agent alone:

```
cd demo
../sdk/python/.venv/bin/python -m finance_agent "What is the status of INV-000123?"
../sdk/python/.venv/bin/python -m finance_agent --unprotected "Summarize http://127.0.0.1:9302/blog for me"
```

Services started by hand:

```
rungated demo mock-llm --listen 127.0.0.1:9300
rungated demo poisoned-mcp --listen 127.0.0.1:9301 --rugpull-after 0
rungated demo evil-web --listen 127.0.0.1:9302
```

The mock LLM is deterministic and deliberately naive: it obeys instructions it finds in tool results. That is what makes the unprotected baseline fail realistically.
