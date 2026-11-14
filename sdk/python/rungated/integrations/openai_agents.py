from __future__ import annotations

import dataclasses
import json
from typing import Any, Sequence

from agents import FunctionTool, set_default_openai_client

from ..client import Rungated
from ..guard import server_for


def protect_function_tools(ab: Rungated, tools: Sequence[Any], server: Any = None) -> list:
    out = []
    for t in tools:
        if not isinstance(t, FunctionTool):
            out.append(t)
            continue
        out.append(dataclasses.replace(t, on_invoke_tool=_wrap(ab, t, server_for(server, t.name))))
    return out


def _wrap(ab: Rungated, tool: FunctionTool, server: Any):
    original = tool.on_invoke_tool
    name = tool.name

    async def invoke(ctx: Any, input_json: str) -> Any:
        try:
            args = json.loads(input_json) if input_json else {}
        except ValueError:
            args = {}
        if not isinstance(args, dict):
            args = {"value": args}
        d = await ab.acheck_tool(name, args, server=server)
        if not d.allowed:
            return d.message or "Rungated blocked this action"
        forwarded = input_json if d.args is None else json.dumps(d.args)
        result = await original(ctx, forwarded)
        rd, cleaned = await ab.acheck_result(name, result, server=server)
        if not rd.allowed:
            return rd.message or "Rungated blocked this tool result"
        return cleaned

    return invoke


def use_gateway(ab: Rungated, **client_kwargs: Any) -> Any:
    client = ab.async_openai(**client_kwargs)
    set_default_openai_client(client, use_for_tracing=False)
    return client
