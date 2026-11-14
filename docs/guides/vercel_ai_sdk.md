# Vercel AI SDK in 10 minutes

Protect the tools of an agent built with the Vercel AI SDK in TypeScript.

## 1. Start the gateway and register the agent

Follow steps 1 and 2 of the [LangGraph guide](langgraph.md). The `agent.yaml` is the same.

## 2. Install

```bash
npm install @rungated/sdk ai @ai-sdk/openai zod
export RUNGATED_GATEWAY=http://localhost:8080
export RUNGATED_AGENT_KEY=<agent key>
```

## 3. Wrap the tools and the model

```ts
import { createOpenAI } from "@ai-sdk/openai";
import { generateText, tool } from "ai";
import { z } from "zod";
import { Rungated, protectAiSdkTools } from "@rungated/sdk";

const rg = new Rungated({ agentId: "support-agent" });
const openai = createOpenAI({ baseURL: rg.llmUrl("openai"), apiKey: "rungated", headers: rg.headers() });

const tools = protectAiSdkTools(
  rg,
  {
    lookup_order: tool({
      description: "Fetch one order by id",
      parameters: z.object({ order_id: z.string() }),
      execute: async ({ order_id }) => `order ${order_id}: shipped`,
    }),
    issue_refund: tool({
      description: "Refund an order",
      parameters: z.object({ order_id: z.string(), amount: z.number() }),
      execute: async ({ order_id, amount }) => `refunded ${amount} on ${order_id}`,
    }),
  },
  { server: "orders" },
);

const { text } = await generateText({
  model: openai("gpt-4o-mini"),
  tools,
  maxSteps: 5,
  prompt: "Refund 40 dollars on ORD-10042",
});
```

`baseURL` and `headers` point the provider at the gateway with the agent identity, so every model call is checked. `protectAiSdkTools` checks every tool call and result. A blocked call returns a refusal string to the model.

Check the field names your installed `ai` version uses for tool schemas (`parameters` or `inputSchema`); Rungated wraps only `execute`, so either works.

## 4. See it work

Open the Console at [http://localhost:8080](http://localhost:8080) and watch **Live**.

## Runnable example

`examples/quickstart/vercel_ai_example.mjs` runs four calls through `protectAiSdkTools` without an OpenAI key:

```bash
bash examples/quickstart/run.sh node
```
