import sys

from langchain_core.messages import AIMessage
from langchain_core.tools import tool
from langgraph.graph import END, START, MessagesState, StateGraph

from rungated import Rungated
from rungated.integrations.langgraph import RungatedToolNode


@tool(description="Fetch one order by id")
def lookup_order(order_id: str) -> str:
    return f"order {order_id}: 2 items, shipped"


@tool(description="Refund an order")
def issue_refund(order_id: str, amount: float) -> str:
    return f"refunded {amount} on {order_id}"


@tool(description="Delete a customer account")
def delete_account(customer_id: str) -> str:
    return f"deleted {customer_id}"


rg = Rungated(agent_id="quickstart-agent", session_id="langgraph-demo")
node = RungatedToolNode(rg, [lookup_order, issue_refund, delete_account], server="orders")

g = StateGraph(MessagesState)
g.add_node("tools", node)
g.add_edge(START, "tools")
g.add_edge("tools", END)
app = g.compile()

calls = [
    ("lookup_order", {"order_id": "ORD-10042"}, True),
    ("issue_refund", {"order_id": "ORD-10042", "amount": 40}, True),
    ("issue_refund", {"order_id": "ORD-10042", "amount": 5000}, False),
    ("delete_account", {"customer_id": "c-77"}, False),
]
msg = AIMessage(content="", tool_calls=[{"name": n, "args": a, "id": f"call{i}"} for i, (n, a, _) in enumerate(calls)])
out = app.invoke({"messages": [msg]})

ok = True
for (name, args, want), m in zip(calls, out["messages"][1:]):
    allowed = m.status != "error"
    mark = "PASS" if allowed == want else "FAIL"
    ok = ok and allowed == want
    print(f"{mark} langgraph {name} {args} -> {'allowed' if allowed else 'blocked'}: {m.content}")
sys.exit(0 if ok else 1)
