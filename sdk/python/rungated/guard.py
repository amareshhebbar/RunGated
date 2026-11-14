from __future__ import annotations

import functools
import inspect
from typing import Any, Callable, Dict, Optional

from .client import Rungated
from .errors import ActionBlocked


def _bind(sig: inspect.Signature, args: tuple, kwargs: dict) -> Dict[str, Any]:
    bound = sig.bind(*args, **kwargs)
    bound.apply_defaults()
    out: Dict[str, Any] = {}
    for name, value in bound.arguments.items():
        p = sig.parameters[name]
        if p.kind is inspect.Parameter.VAR_KEYWORD:
            out.update(value)
        elif p.kind is inspect.Parameter.VAR_POSITIONAL:
            out[name] = list(value)
        else:
            out[name] = value
    return out


def _rebind(sig: inspect.Signature, args: tuple, kwargs: dict, restored: Optional[Dict[str, Any]]) -> tuple:
    if not restored:
        return args, kwargs
    bound = sig.bind(*args, **kwargs)
    for name in list(bound.arguments):
        p = sig.parameters[name]
        if p.kind is inspect.Parameter.VAR_KEYWORD:
            bound.arguments[name] = {k: restored.get(k, v) for k, v in bound.arguments[name].items()}
        elif p.kind is inspect.Parameter.VAR_POSITIONAL:
            if name in restored:
                bound.arguments[name] = tuple(restored[name])
        elif name in restored:
            bound.arguments[name] = restored[name]
    return bound.args, bound.kwargs


def server_for(server: Any, name: str) -> Optional[str]:
    if server is None or isinstance(server, str):
        return server
    return server.get(name)


def protect(
    ab: Rungated,
    fn: Optional[Callable] = None,
    *,
    name: Optional[str] = None,
    server: Optional[str] = None,
    method: Optional[str] = None,
    on_block: str = "raise",
    scan_result: bool = True,
):
    def deco(f: Callable) -> Callable:
        tool = name or f.__name__
        sig = inspect.signature(f)

        def refuse(d):
            if on_block == "message":
                return d.message or "Rungated blocked this action"
            raise ActionBlocked(d) if d.verdict not in ("contain", "hold") else _specific(d)

        if inspect.iscoroutinefunction(f):

            @functools.wraps(f)
            async def awrapper(*args: Any, **kwargs: Any) -> Any:
                d = await ab.acheck_tool(tool, _bind(sig, args, kwargs), server=server, method=method)
                if not d.allowed:
                    return refuse(d)
                a, k = _rebind(sig, args, kwargs, d.args)
                out = await f(*a, **k)
                if not scan_result:
                    return out
                rd, cleaned = await ab.acheck_result(tool, out, server=server)
                if not rd.allowed:
                    return refuse(rd)
                return cleaned

            return awrapper

        @functools.wraps(f)
        def wrapper(*args: Any, **kwargs: Any) -> Any:
            d = ab.check_tool(tool, _bind(sig, args, kwargs), server=server, method=method)
            if not d.allowed:
                return refuse(d)
            a, k = _rebind(sig, args, kwargs, d.args)
            out = f(*a, **k)
            if not scan_result:
                return out
            rd, cleaned = ab.check_result(tool, out, server=server)
            if not rd.allowed:
                return refuse(rd)
            return cleaned

        return wrapper

    if fn is not None:
        return deco(fn)
    return deco


def _specific(d):
    from .errors import AgentContained, ApprovalDenied

    if d.verdict == "contain":
        raise AgentContained(d)
    raise ApprovalDenied(d)
