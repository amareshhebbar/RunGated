import asyncio
import json
import sys

from agents import function_tool
from agents.tool_context import ToolContext

from rungated import Rungated
from rungated.integrations.openai_agents import protect_function_tools


@function_tool
def lookup_order(order_id: str) -> str:
    return f"order {order_id}: 2 items, shipped"


@function_tool
def issue_refund(order_id: str, amount: float) -> str:
    return f"refunded {amount} on {order_id}"


@function_tool
def delete_account(customer_id: str) -> str:
    return f"deleted {customer_id}"


async def main() -> int:
    rg = Rungated(agent_id="quickstart-agent", session_id="openai-agents-demo")
    tools = {t.name: t for t in protect_function_tools(rg, [lookup_order, issue_refund, delete_account], server="orders")}
    calls = [
        ("lookup_order", {"order_id": "ORD-10042"}, True),
        ("issue_refund", {"order_id": "ORD-10042", "amount": 40}, True),
        ("issue_refund", {"order_id": "ORD-10042", "amount": 5000}, False),
        ("delete_account", {"customer_id": "c-77"}, False),
    ]
    ok = True
    for name, args, want in calls:
        raw = json.dumps(args)
        ctx = ToolContext(context=None, tool_name=name, tool_call_id="x", tool_arguments=raw)
        out = await tools[name].on_invoke_tool(ctx, raw)
        allowed = "Rungated" not in str(out)
        mark = "PASS" if allowed == want else "FAIL"
        ok = ok and allowed == want
        print(f"{mark} openai-agents {name} {args} -> {'allowed' if allowed else 'blocked'}: {out}")
    return 0 if ok else 1


sys.exit(asyncio.run(main()))
