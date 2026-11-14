import { DynamicStructuredTool } from "@langchain/core/tools";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { z } from "zod";
import { Rungated, protectAiSdkTools, protectLangChainTools, protectOpenAIAgentTools } from "../src/index.js";
import { buildBinary, startGateway, type Gateway } from "./harness.js";

let gw: Gateway;
let ab: Rungated;
const SERVERS = { get_invoice: "finance-mcp" };

beforeAll(async () => {
  gw = await startGateway(buildBinary());
  ab = new Rungated({ gateway: gw.url, agentId: "finance-agent", sessionId: "ts-tools" });
});

afterAll(async () => {
  await gw.stop();
});

describe("framework adapters", () => {
  it("langchain tools", async () => {
    const getInvoice = new DynamicStructuredTool({ name: "get_invoice", description: "Fetch one invoice", schema: z.object({ invoice_id: z.string() }), func: async ({ invoice_id }) => "ok " + invoice_id });
    const wipe = new DynamicStructuredTool({ name: "wipe_database", description: "Dangerous", schema: z.object({}), func: async () => "wiped" });
    const [safeInvoice, safeWipe] = protectLangChainTools(ab, [getInvoice, wipe] as any[], { server: SERVERS }) as any[];
    expect(await safeInvoice.invoke({ invoice_id: "INV-000123" })).toBe("ok INV-000123");
    const refused = await safeWipe.invoke({});
    expect(String(refused)).toContain("Rungated");
    expect(String(refused)).not.toBe("wiped");
  });

  it("langchain tool call objects", async () => {
    const getInvoice = new DynamicStructuredTool({ name: "get_invoice", description: "Fetch one invoice", schema: z.object({ invoice_id: z.string() }), func: async ({ invoice_id }) => "ok " + invoice_id });
    const [safe] = protectLangChainTools(ab, [getInvoice] as any[], { server: SERVERS }) as any[];
    const out: any = await safe.invoke({ type: "tool_call", id: "c1", name: "get_invoice", args: { invoice_id: "INV-000123" } });
    expect(out.content ?? out).toContain("ok INV-000123");
  });

  it("vercel ai sdk style tools", async () => {
    const tools = {
      get_invoice: { description: "x", execute: async (a: { invoice_id: string }) => "ok " + a.invoice_id },
      wipe_database: { description: "y", execute: async () => "wiped" },
      passive: { description: "no execute" },
    };
    const safe = protectAiSdkTools(ab, tools, { server: SERVERS }) as any;
    expect(await safe.get_invoice.execute({ invoice_id: "INV-000123" })).toBe("ok INV-000123");
    expect(String(await safe.wipe_database.execute({}))).toContain("Rungated");
    expect(safe.passive.execute).toBeUndefined();
  });

  it("openai agents style tools", async () => {
    const mk = (name: string, fn: (a: any) => string) => ({ name, invoke: async (_ctx: unknown, input: string) => fn(JSON.parse(input || "{}")) });
    const [safeInvoice, safeWipe] = protectOpenAIAgentTools(ab, [mk("get_invoice", (a) => "ok " + a.invoice_id), mk("wipe_database", () => "wiped")], { server: SERVERS });
    expect(await safeInvoice.invoke!({}, JSON.stringify({ invoice_id: "INV-000123" }))).toBe("ok INV-000123");
    expect(String(await safeWipe.invoke!({}, "{}"))).toContain("Rungated");
  });
});
