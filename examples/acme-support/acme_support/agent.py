import os

from rungated import Rungated, protect

ORDERS = {
    "ORD-10001": {"order_id": "ORD-10001", "status": "shipped", "total": 42.5, "email": "jo.buyer@northwind.example"},
    "ORD-10002": {"order_id": "ORD-10002", "status": "delivered", "total": 99.0, "email": "sam.client@northwind.example"},
}
REFUNDS = []


def build(gateway=None, agent_key=None):
    rg = Rungated(
        gateway=gateway or os.environ.get("RUNGATED_GATEWAY", "http://127.0.0.1:8080"),
        agent_id="acme-support",
        agent_key=agent_key or os.environ.get("RUNGATED_AGENT_KEY"),
    )

    @protect(rg, name="lookup_order", server="orders-mcp")
    def lookup_order(order_id: str) -> dict:
        return ORDERS[order_id]

    @protect(rg, name="issue_refund", server="orders-mcp")
    def issue_refund(order_id: str, amount: float) -> dict:
        REFUNDS.append((order_id, amount))
        return {"order_id": order_id, "refunded": amount}

    @protect(rg, name="delete_customer", server="orders-mcp")
    def delete_customer(customer_id: str) -> dict:
        return {"deleted": customer_id}

    return rg, lookup_order, issue_refund, delete_customer


def ask_model(rg, question, model="mock-1"):
    client = rg.openai()
    resp = client.chat.completions.create(model=model, messages=[{"role": "user", "content": question}])
    return resp.choices[0].message.content or ""
