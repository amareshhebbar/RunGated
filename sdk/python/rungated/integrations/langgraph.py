from __future__ import annotations

from typing import Any, Callable, Sequence

from langchain_core.messages import ToolMessage
from langchain_core.tools import BaseTool, StructuredTool
from langgraph.prebuilt import ToolNode

from ..client import Rungated, collect_strings
from ..guard import protect, server_for


def _refusal(request: Any, message: str) -> ToolMessage:
    call = request.tool_call
    return ToolMessage(content=message, tool_call_id=call["id"], name=call["name"], status="error")


def tool_call_wrappers(ab: Rungated, server: Any = None):
    def sync_wrap(request: Any, execute: Callable) -> Any:
        call = request.tool_call
        d = ab.check_tool(call["name"], call.get("args") or {}, server=server_for(server, call["name"]))
        if not d.allowed:
            return _refusal(request, d.message or "Rungated blocked this action")
        if d.args is not None:
            request = request.override(tool_call={**call, "args": d.args})
        out = execute(request)
        if not isinstance(out, ToolMessage):
            return out
        strings = collect_strings(out.content)
        if not strings:
            return out
        rd, cleaned = ab.check_result(call["name"], out.content, server=server_for(server, call["name"]))
        if not rd.allowed:
            return _refusal(request, rd.message or "Rungated blocked this tool result")
        if cleaned is not out.content:
            return out.model_copy(update={"content": cleaned})
        return out

    async def async_wrap(request: Any, execute: Callable) -> Any:
        call = request.tool_call
        d = await ab.acheck_tool(call["name"], call.get("args") or {}, server=server_for(server, call["name"]))
        if not d.allowed:
            return _refusal(request, d.message or "Rungated blocked this action")
        if d.args is not None:
            request = request.override(tool_call={**call, "args": d.args})
        out = await execute(request)
        if not isinstance(out, ToolMessage):
            return out
        strings = collect_strings(out.content)
        if not strings:
            return out
        rd, cleaned = await ab.acheck_result(call["name"], out.content, server=server_for(server, call["name"]))
        if not rd.allowed:
            return _refusal(request, rd.message or "Rungated blocked this tool result")
        if cleaned is not out.content:
            return out.model_copy(update={"content": cleaned})
        return out

    return sync_wrap, async_wrap


def RungatedToolNode(ab: Rungated, tools: Sequence[Any], server: Any = None, **kwargs: Any) -> ToolNode:
    sync_wrap, async_wrap = tool_call_wrappers(ab, server)
    return ToolNode(tools, wrap_tool_call=sync_wrap, awrap_tool_call=async_wrap, **kwargs)


def protect_tools(ab: Rungated, tools: Sequence[BaseTool], server: Any = None, on_block: str = "message") -> list:
    out = []
    for t in tools:
        if isinstance(t, StructuredTool):
            update = {}
            if t.func is not None:
                update["func"] = protect(ab, t.func, name=t.name, server=server_for(server, t.name), on_block=on_block)
            if t.coroutine is not None:
                update["coroutine"] = protect(ab, t.coroutine, name=t.name, server=server_for(server, t.name), on_block=on_block)
            out.append(t.model_copy(update=update))
        else:
            raise TypeError("protect_tools supports StructuredTool, use RungatedToolNode for other tool classes: %r" % type(t))
    return out


def llm_kwargs(ab: Rungated, provider: str = "openai") -> dict:
    return ab.llm_kwargs(provider)
