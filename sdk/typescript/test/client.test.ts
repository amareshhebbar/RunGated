import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { ActionBlocked, Rungated, ApprovalDenied, GatewayError, protect } from "../src/index.js";
import { buildBinary, financeManifestWithTransfer, startGateway, type Gateway } from "./harness.js";

let gw: Gateway;
let ab: Rungated;
const EMAIL = "carol.customer@clientmail.example";

beforeAll(async () => {
  gw = await startGateway(buildBinary());
  ab = new Rungated({ gateway: gw.url, agentId: "finance-agent", sessionId: "ts-1" });
});

afterAll(async () => {
  await gw.stop();
});

async function firstPending(admin: Rungated): Promise<string> {
  for (let i = 0; i < 150; i++) {
    const r = await fetch(gw.url + "/v1/approvals?status=pending");
    const j: any = await r.json();
    if (j.approvals?.length) return j.approvals[0].id;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("no pending approval");
}

describe("client", () => {
  it("health and allow", async () => {
    expect((await ab.health()).status).toBeTruthy();
    const d = await ab.checkTool("get_invoice", { invoice_id: "INV-000123" }, { server: "finance-mcp" });
    expect(d.allowed).toBe(true);
    expect(d.verdict).toBe("allow");
  });

  it("blocks unknown tools and throws in enforce mode", async () => {
    const d = await ab.checkTool("wipe_database", {});
    expect(d.blocked).toBe(true);
    await expect(ab.checkTool("wipe_database", {}, { enforce: true })).rejects.toBeInstanceOf(ActionBlocked);
  });

  it("blocks arg constraint violations", async () => {
    const d = await ab.checkTool("get_invoice", { invoice_id: "bad" }, { server: "finance-mcp" });
    expect(d.blocked).toBe(true);
  });

  it("redacts and restores", async () => {
    const d = await ab.scan("Please email " + EMAIL + " about INV-000123", { model: "mock-1", provider: "mock" });
    expect(d.allowed).toBe(true);
    expect(d.redactedTexts?.[0]).not.toContain(EMAIL);
    const back = await ab.restore(d.redactedTexts![0]);
    expect(back).toContain(EMAIL);
    const obj = await ab.restore({ a: [d.redactedTexts![0]] });
    expect(obj.a[0]).toContain(EMAIL);
  });

  it("protect allows and blocks", async () => {
    const getInvoice = protect(ab, async (a: { invoice_id: string }) => ({ id: a.invoice_id }), { name: "get_invoice", server: "finance-mcp" });
    expect(await getInvoice({ invoice_id: "INV-000123" })).toEqual({ id: "INV-000123" });
    let ran = false;
    const wipe = protect(ab, async (_a: Record<string, never>) => {
      ran = true;
      return "boom";
    }, { name: "wipe_database" });
    await expect(wipe({})).rejects.toBeInstanceOf(ActionBlocked);
    expect(ran).toBe(false);
    const soft = protect(ab, async (_a: Record<string, never>) => "ran", { name: "nope_tool", onBlock: "message" });
    expect(String(await soft({}))).toContain("Rungated");
  });

  it("scans tool results", async () => {
    const r = await ab.checkResult("get_invoice", { rows: ["contact " + EMAIL, "plain row"] });
    expect(r.decision.allowed).toBe(true);
    expect(r.result.rows[1]).toBe("plain row");
    if (r.decision.redactedTexts) expect(r.result.rows[0]).not.toContain(EMAIL);
  });

  it("fails closed or open when the gateway is down", async () => {
    const closed = new Rungated({ gateway: "http://127.0.0.1:9", agentId: "x", timeoutMs: 1000 });
    await expect(closed.checkTool("t", {})).rejects.toBeInstanceOf(GatewayError);
    const open = new Rungated({ gateway: "http://127.0.0.1:9", agentId: "x", timeoutMs: 1000, failOpen: true });
    const d = await open.checkTool("t", {});
    expect(d.allowed).toBe(true);
    expect(d.reasons[0]).toContain("failing open");
  });

  it("issues and uses agent tokens", async () => {
    const t = await ab.issueToken("tok-1", 60);
    expect(t.token.startsWith("rgt1.")).toBe(true);
    const tc = new Rungated({ gateway: gw.url, token: t.token });
    const d = await tc.checkTool("get_invoice", { invoice_id: "INV-000123" }, { server: "finance-mcp" });
    expect(d.allowed).toBe(true);
  });

  it("holds until approved and raises when denied", async () => {
    const admin = new Rungated({ gateway: gw.url });
    await admin.register(financeManifestWithTransfer());
    const appr = new Rungated({ gateway: gw.url, agentId: "sdk-approver", sessionId: "appr", holdTimeoutMs: 30000 });
    const pending = appr.checkTool("transfer", { amount: 250 }, { server: "finance-mcp" });
    const id = await firstPending(admin);
    await fetch(gw.url + "/v1/approvals/" + id + "/approve", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ reason: "ok" }) });
    const d = await pending;
    expect(d.allowed).toBe(true);
    expect(d.approvalStatus).toBe("approved");

    const denied = appr.checkTool("transfer", { amount: 99 }, { server: "finance-mcp", enforce: true });
    const denial = denied.catch((e) => e);
    const id2 = await firstPending(admin);
    await fetch(gw.url + "/v1/approvals/" + id2 + "/deny", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ reason: "no" }) });
    expect(await denial).toBeInstanceOf(ApprovalDenied);

    const nowait = await appr.checkTool("transfer", { amount: 5 }, { server: "finance-mcp", wait: false });
    expect(nowait.verdict).toBe("hold");
    expect(nowait.blocked).toBe(true);
    expect(nowait.approvalId).toBeTruthy();
  });
});
