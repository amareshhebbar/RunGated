import json

import pytest
from langchain_core.messages import AIMessage
from langchain_core.tools import tool

from langgraph.graph import END, START, MessagesState, StateGraph

from rungated.integrations.langgraph import RungatedToolNode, protect_tools

def graph(node):
    g = StateGraph(MessagesState)
    g.add_node("tools", node)
    g.add_edge(START, "tools")
    g.add_edge("tools", END)
    return g.compile()


SERVERS = {"get_invoice": "finance-mcp"}


def make_tools():
    @tool(description="Fetch one invoice")
    def get_invoice(invoice_id: str) -> str:
        return "invoice " + invoice_id + " for carol.customer@clientmail.example"

    @tool(description="Dangerous")
    def wipe_database() -> str:
        return "wiped"

    return get_invoice, wipe_database


def call_msg(*calls):
    return {"messages": [AIMessage(content="", tool_calls=[{"name": n, "args": a, "id": "c%d" % i} for i, (n, a) in enumerate(calls)])]}


def test_toolnode_allows_blocks_and_scans(ab):
    node = RungatedToolNode(ab, list(make_tools()), server=SERVERS)
    out = graph(node).invoke(call_msg(("get_invoice", {"invoice_id": "INV-000123"}), ("wipe_database", {})))
    msgs = out["messages"][1:]
    assert msgs[0].status != "error" and "INV-000123" in msgs[0].content
    assert msgs[1].status == "error" and "Rungated" in msgs[1].content
    assert "wiped" not in msgs[1].content


async def test_toolnode_async(ab):
    node = RungatedToolNode(ab, list(make_tools()), server=SERVERS)
    out = await graph(node).ainvoke(call_msg(("get_invoice", {"invoice_id": "INV-000123"}), ("wipe_database", {})))
    msgs = out["messages"][1:]
    assert "INV-000123" in msgs[0].content
    assert msgs[1].status == "error"


def test_toolnode_bad_args_blocked(ab):
    node = RungatedToolNode(ab, list(make_tools()), server=SERVERS)
    out = graph(node).invoke(call_msg(("get_invoice", {"invoice_id": "DROP TABLE"})))
    assert out["messages"][1].status == "error"


def test_protect_tools_structured(ab):
    from langchain_core.tools import StructuredTool

    def get_invoice(invoice_id: str) -> str:
        return "ok " + invoice_id

    def wipe_database() -> str:
        return "wiped"

    tools = [StructuredTool.from_function(get_invoice, description="inv"), StructuredTool.from_function(wipe_database, description="wipe")]
    safe = protect_tools(ab, tools, server=SERVERS)
    assert safe[0].invoke({"invoice_id": "INV-000123"}) == "ok INV-000123"
    refused = safe[1].invoke({})
    assert "Rungated" in refused and refused != "wiped"


async def test_openai_agents_function_tools(ab):
    from agents import function_tool
    from agents.tool_context import ToolContext

    from rungated.integrations.openai_agents import protect_function_tools

    @function_tool
    def get_invoice(invoice_id: str) -> str:
        return "ok " + invoice_id

    @function_tool
    def wipe_database() -> str:
        return "wiped"

    safe = protect_function_tools(ab, [get_invoice, wipe_database], server=SERVERS)

    def ctx(name, args):
        return ToolContext(context=None, tool_name=name, tool_call_id="x", tool_arguments=args)

    ok = await safe[0].on_invoke_tool(ctx("get_invoice", json.dumps({"invoice_id": "INV-000123"})), json.dumps({"invoice_id": "INV-000123"}))
    assert ok == "ok INV-000123"
    refused = await safe[1].on_invoke_tool(ctx("wipe_database", "{}"), "{}")
    assert "Rungated" in refused


async def test_use_gateway_sets_default_client(ab):
    from rungated.integrations.openai_agents import use_gateway

    client = use_gateway(ab)
    assert str(client.base_url).startswith(ab.gateway)
