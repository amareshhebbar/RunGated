import { Rungated, protectAiSdkTools } from "@rungated/sdk";

const rg = new Rungated({ agentId: "quickstart-agent", sessionId: "vercel-ai-demo" });

const tools = protectAiSdkTools(
  rg,
  {
    lookup_order: { description: "Fetch one order by id", execute: async ({ order_id }) => `order ${order_id}: 2 items, shipped` },
    issue_refund: { description: "Refund an order", execute: async ({ order_id, amount }) => `refunded ${amount} on ${order_id}` },
    delete_account: { description: "Delete a customer account", execute: async ({ customer_id }) => `deleted ${customer_id}` },
  },
  { server: "orders" },
);

const calls = [
  ["lookup_order", { order_id: "ORD-10042" }, true],
  ["issue_refund", { order_id: "ORD-10042", amount: 40 }, true],
  ["issue_refund", { order_id: "ORD-10042", amount: 5000 }, false],
  ["delete_account", { customer_id: "c-77" }, false],
];

let ok = true;
for (const [name, args, want] of calls) {
  const out = await tools[name].execute(args, {});
  const allowed = !String(out).includes("Rungated");
  ok = ok && allowed === want;
  console.log(`${allowed === want ? "PASS" : "FAIL"} vercel-ai ${name} ${JSON.stringify(args)} -> ${allowed ? "allowed" : "blocked"}: ${out}`);
}
process.exit(ok ? 0 : 1);
