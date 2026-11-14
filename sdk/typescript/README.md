# @rungated/sdk

TypeScript SDK for the Rungated runtime AI agent firewall. No runtime dependencies, Node 18 or newer.

```ts
import OpenAI from "openai";
import { Rungated, protect } from "@rungated/sdk";

const ab = new Rungated({ gateway: "http://127.0.0.1:8080", agentId: "finance-agent", agentKey: "..." });

const openai = new OpenAI(ab.llmOptions("openai"));

const getInvoice = protect(ab, async (a: { invoice_id: string }) => lookup(a.invoice_id), { name: "get_invoice", server: "finance-mcp" });
```

Adapters: `protectLangChainTools`, `protectAiSdkTools`, `protectOpenAIAgentTools`.

Environment variables: RUNGATED_GATEWAY, RUNGATED_AGENT, RUNGATED_AGENT_KEY, RUNGATED_TOKEN, RUNGATED_API_KEY, RUNGATED_SESSION.
