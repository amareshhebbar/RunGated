import Anthropic from "@anthropic-ai/sdk";
import OpenAI from "openai";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Rungated } from "../src/index.js";
import { buildBinary, startGateway, type Gateway } from "./harness.js";

let gw: Gateway;
let ab: Rungated;
const PII = "Please email carol.customer@clientmail.example about INV-000123";
const EMAIL = "carol.customer@clientmail.example";

beforeAll(async () => {
  gw = await startGateway(buildBinary());
  ab = new Rungated({ gateway: gw.url, agentId: "finance-agent", sessionId: "ts-llm" });
});

afterAll(async () => {
  await gw.stop();
});

describe("llm through the gateway", () => {
  it("openai client restores PII and strips the blocked tool call", async () => {
    const client = new OpenAI(ab.llmOptions("openai"));
    const r = await client.chat.completions.create({ model: "mock-1", messages: [{ role: "user", content: PII }] });
    const m = r.choices[0].message;
    expect(m.content).toContain(EMAIL);
    expect((m.tool_calls ?? []).map((t: any) => t.function.name)).toEqual(["get_invoice"]);
    expect(gw.upstream.seen.at(-1)!.body).not.toContain("carol.customer");
  });

  it("anthropic client restores PII and strips the blocked tool use", async () => {
    const client = new Anthropic(ab.llmOptions("anthropic"));
    const r = await client.messages.create({ model: "claude-sonnet-4", max_tokens: 50, messages: [{ role: "user", content: PII }] });
    expect(r.content.map((b) => b.type)).toEqual(["text", "tool_use", "text"]);
    expect((r.content[0] as any).text).toContain(EMAIL);
    expect(gw.upstream.seen.at(-1)!.body).not.toContain("carol.customer");
  });

  it("a forbidden model is refused with 403", async () => {
    const client = new OpenAI(ab.llmOptions("openai"));
    await expect(client.chat.completions.create({ model: "forbidden-model", messages: [{ role: "user", content: "hi" }] })).rejects.toMatchObject({ status: 403 });
  });
});
