# MCP

Rungated sits between your agent and its MCP servers. Every message is checked, tool descriptions are pinned, and results are scanned before the agent sees them.

## HTTP MCP servers

Declare each server in the agent manifest, then point the agent at `http://localhost:8080/mcp/<server>` instead of the real URL.

```yaml
mcpServers:
  - name: finance-mcp
    url: https://mcp.internal.example/mcp
    headers:
      Authorization: Bearer ${FINANCE_MCP_TOKEN}
```

 Send the agent identity headers `X-Rungated-Agent` and `X-Rungated-Token` (or `X-Rungated-Key`).

## stdio MCP servers

Wrap the command with `mcp-stdio`:

```bash
rungated mcp-stdio --gateway http://localhost:8080 --server finance-mcp \
  --agent finance-agent --key "$RUNGATED_AGENT_KEY" -- node ./finance-server.js
```

The wrapper speaks stdio to your agent and passes each message through the gateway.

## Tool pinning

The first time a tool is seen, its description and schema are pinned. If they change later, the tool is held until a person approves or rejects the change. This stops a server from quietly rewriting a tool after you trusted it. See `/v1/pins` in the [API reference](../reference/api.md).

## Result scanning

Tool results are checked for injected instructions and secrets before they return to the agent.
