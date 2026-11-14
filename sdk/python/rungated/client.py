from __future__ import annotations

import asyncio
import copy
import os
from typing import Any, Dict, List, Mapping, Optional

import httpx

from .errors import ActionBlocked, AgentContained, RungatedError, ApprovalDenied, GatewayError
from .types import Decision

DEFAULT_GATEWAY = "http://127.0.0.1:8080"


def _env(name: str) -> Optional[str]:
    v = os.environ.get(name)
    return v if v else None


def collect_strings(obj: Any, path: tuple = ()) -> List[tuple]:
    out: List[tuple] = []
    if isinstance(obj, str):
        out.append((path, obj))
    elif isinstance(obj, Mapping):
        for k, v in obj.items():
            out.extend(collect_strings(v, path + (k,)))
    elif isinstance(obj, (list, tuple)):
        for i, v in enumerate(obj):
            out.extend(collect_strings(v, path + (i,)))
    return out


def replace_strings(obj: Any, replacements: List[str], strings: List[tuple]) -> Any:
    if not strings:
        return obj
    if strings[0][0] == ():
        return replacements[0]
    root = copy.deepcopy(obj)
    for (path, _), new in zip(strings, replacements):
        cur = root
        for key in path[:-1]:
            cur = cur[key]
        last = path[-1]
        if isinstance(cur, tuple):
            continue
        cur[last] = new
    return root


class Rungated:
    def __init__(
        self,
        gateway: Optional[str] = None,
        agent_id: Optional[str] = None,
        agent_key: Optional[str] = None,
        token: Optional[str] = None,
        api_key: Optional[str] = None,
        session_id: Optional[str] = None,
        timeout: float = 30.0,
        hold_timeout: float = 600.0,
        fail_open: bool = False,
        wait: bool = True,
        transport: Optional[httpx.BaseTransport] = None,
    ):
        self.gateway = (gateway or _env("RUNGATED_GATEWAY") or DEFAULT_GATEWAY).rstrip("/")
        self.agent_id = agent_id or _env("RUNGATED_AGENT")
        self.agent_key = agent_key or _env("RUNGATED_AGENT_KEY")
        self.token = token or _env("RUNGATED_TOKEN")
        self.api_key = api_key or _env("RUNGATED_API_KEY")
        self.session_id = session_id or _env("RUNGATED_SESSION")
        self.timeout = timeout
        self.hold_timeout = hold_timeout
        self.fail_open = fail_open
        self.wait = wait
        self._transport = transport
        self._http: Optional[httpx.Client] = None
        self._ahttp: Dict[int, httpx.AsyncClient] = {}

    def with_session(self, session_id: str) -> "Rungated":
        c = copy.copy(self)
        c.session_id = session_id
        c._http = None
        c._ahttp = {}
        return c

    def headers(self) -> Dict[str, str]:
        h: Dict[str, str] = {}
        if self.token:
            h["X-Rungated-Token"] = self.token
        else:
            if self.agent_id:
                h["X-Rungated-Agent"] = self.agent_id
            if self.agent_key:
                h["X-Rungated-Key"] = self.agent_key
        if self.api_key:
            h["Authorization"] = "Bearer " + self.api_key
        if self.session_id:
            h["X-Rungated-Session"] = self.session_id
        return h

    def llm_url(self, provider: str) -> str:
        if provider == "openai":
            return self.gateway + "/llm/openai/v1"
        if provider == "anthropic":
            return self.gateway + "/llm/anthropic"
        raise ValueError("provider must be openai or anthropic")

    def llm_kwargs(self, provider: str) -> Dict[str, Any]:
        return {"base_url": self.llm_url(provider), "default_headers": self.headers()}

    def _client(self) -> httpx.Client:
        if self._http is None:
            self._http = httpx.Client(timeout=httpx.Timeout(self.timeout, read=max(self.timeout, self.hold_timeout)), transport=self._transport)
        return self._http

    def _aclient(self) -> httpx.AsyncClient:
        loop = asyncio.get_running_loop()
        key = id(loop)
        c = self._ahttp.get(key)
        if c is None or c.is_closed:
            c = httpx.AsyncClient(timeout=httpx.Timeout(self.timeout, read=max(self.timeout, self.hold_timeout)))
            self._ahttp[key] = c
        return c

    def close(self) -> None:
        if self._http is not None:
            self._http.close()
            self._http = None

    async def aclose(self) -> None:
        loop = asyncio.get_running_loop()
        c = self._ahttp.pop(id(loop), None)
        if c is not None:
            await c.aclose()

    def __enter__(self) -> "Rungated":
        return self

    def __exit__(self, *exc: Any) -> None:
        self.close()

    async def __aenter__(self) -> "Rungated":
        return self

    async def __aexit__(self, *exc: Any) -> None:
        await self.aclose()

    @staticmethod
    def _payload(kind: str, **kw: Any) -> Dict[str, Any]:
        body: Dict[str, Any] = {"kind": kind}
        for k, v in kw.items():
            if v is not None and v != "" and v != {}:
                body[k] = v
        return body

    def _decide_body(self, kind: str, kw: Dict[str, Any]) -> Dict[str, Any]:
        body = self._payload(kind, **kw)
        if self.agent_id and "agent_id" not in body:
            body["agent_id"] = self.agent_id
        if self.session_id and "session_id" not in body:
            body["session_id"] = self.session_id
        return body

    @staticmethod
    def _raise_for(resp: httpx.Response) -> Any:
        try:
            data = resp.json()
        except ValueError:
            data = resp.text
        if resp.status_code >= 400:
            msg = data.get("error") if isinstance(data, dict) else None
            raise GatewayError(msg or "gateway returned HTTP %d" % resp.status_code, resp.status_code, data)
        return data

    def _open(self, exc: Exception) -> Decision:
        if self.fail_open:
            return Decision.open_allow("gateway unreachable, failing open: %s" % exc)
        raise GatewayError("gateway unreachable: %s" % exc) from exc

    def decide(self, kind: str, **kw: Any) -> Decision:
        wait = kw.pop("wait", self.wait)
        body = self._decide_body(kind, kw)
        try:
            resp = self._client().post(self.gateway + "/v1/decide", json=body, params={"wait": "true"} if wait else None, headers=self.headers())
        except httpx.TransportError as e:
            return self._open(e)
        return Decision.from_json(self._raise_for(resp))

    async def adecide(self, kind: str, **kw: Any) -> Decision:
        wait = kw.pop("wait", self.wait)
        body = self._decide_body(kind, kw)
        try:
            resp = await self._aclient().post(self.gateway + "/v1/decide", json=body, params={"wait": "true"} if wait else None, headers=self.headers())
        except httpx.TransportError as e:
            return self._open(e)
        return Decision.from_json(self._raise_for(resp))

    @staticmethod
    def _enforce(d: Decision) -> Decision:
        if d.allowed:
            return d
        if d.verdict == "contain":
            raise AgentContained(d)
        if d.verdict == "hold":
            raise ApprovalDenied(d)
        raise ActionBlocked(d)

    def check_tool(self, tool: str, args: Optional[Dict[str, Any]] = None, server: Optional[str] = None, method: Optional[str] = None, enforce: bool = False, **kw: Any) -> Decision:
        d = self.decide("tool.call", tool=tool, args=args, server=server, method=method, **kw)
        return self._enforce(d) if enforce else d

    async def acheck_tool(self, tool: str, args: Optional[Dict[str, Any]] = None, server: Optional[str] = None, method: Optional[str] = None, enforce: bool = False, **kw: Any) -> Decision:
        d = await self.adecide("tool.call", tool=tool, args=args, server=server, method=method, **kw)
        return self._enforce(d) if enforce else d

    def check_result(self, tool: str, result: Any, enforce: bool = False, server: Optional[str] = None) -> tuple:
        strings = collect_strings(result)
        if not strings:
            return Decision.open_allow("no text in result"), result
        texts = [{"source": "tool_result", "text": s, "path": ".".join(str(p) for p in path)} for path, s in strings]
        d = self.decide("tool.result", tool=tool, server=server, texts=texts)
        if enforce:
            self._enforce(d)
        return d, self._apply(d, result, strings)

    async def acheck_result(self, tool: str, result: Any, enforce: bool = False, server: Optional[str] = None) -> tuple:
        strings = collect_strings(result)
        if not strings:
            return Decision.open_allow("no text in result"), result
        texts = [{"source": "tool_result", "text": s, "path": ".".join(str(p) for p in path)} for path, s in strings]
        d = await self.adecide("tool.result", tool=tool, server=server, texts=texts)
        if enforce:
            self._enforce(d)
        return d, self._apply(d, result, strings)

    @staticmethod
    def _apply(d: Decision, result: Any, strings: List[tuple]) -> Any:
        if d.allowed and d.redacted_texts and len(d.redacted_texts) == len(strings):
            return replace_strings(result, d.redacted_texts, strings)
        return result

    def scan(self, text: str, source: str = "user", kind: str = "llm.request", enforce: bool = False, **kw: Any) -> Decision:
        d = self.decide(kind, texts=[{"source": source, "text": text}], **kw)
        return self._enforce(d) if enforce else d

    async def ascan(self, text: str, source: str = "user", kind: str = "llm.request", enforce: bool = False, **kw: Any) -> Decision:
        d = await self.adecide(kind, texts=[{"source": source, "text": text}], **kw)
        return self._enforce(d) if enforce else d

    def restore(self, value: Any) -> Any:
        body: Dict[str, Any] = {"agent_id": self.agent_id, "session_id": self.session_id}
        if isinstance(value, str):
            body["text"] = value
        else:
            body["value"] = value
        resp = self._client().post(self.gateway + "/v1/restore", json=body, headers=self.headers())
        data = self._raise_for(resp)
        return data["text"] if isinstance(value, str) else data["value"]

    async def arestore(self, value: Any) -> Any:
        body: Dict[str, Any] = {"agent_id": self.agent_id, "session_id": self.session_id}
        if isinstance(value, str):
            body["text"] = value
        else:
            body["value"] = value
        resp = await self._aclient().post(self.gateway + "/v1/restore", json=body, headers=self.headers())
        data = self._raise_for(resp)
        return data["text"] if isinstance(value, str) else data["value"]

    def register(self, manifest: str, rotate_key: bool = False) -> Dict[str, Any]:
        if "\n" not in manifest and os.path.exists(manifest):
            with open(manifest, "r", encoding="utf-8") as f:
                manifest = f.read()
        resp = self._client().post(
            self.gateway + "/v1/agents",
            content=manifest.encode(),
            params={"rotate_key": "true"} if rotate_key else None,
            headers={**self.headers(), "Content-Type": "application/yaml"},
        )
        return self._raise_for(resp)

    def issue_token(self, session_id: Optional[str] = None, ttl_seconds: int = 0) -> Dict[str, Any]:
        if not self.agent_id:
            raise RungatedError("agent_id is required")
        body: Dict[str, Any] = {"session_id": session_id or self.session_id or "", "ttl_seconds": ttl_seconds}
        hdrs = {k: v for k, v in self.headers().items() if k != "X-Rungated-Token"}
        resp = self._client().post(self.gateway + "/v1/agents/%s/token" % self.agent_id, json=body, headers=hdrs)
        return self._raise_for(resp)

    def health(self) -> Dict[str, Any]:
        return self._raise_for(self._client().get(self.gateway + "/v1/health"))

    def feedback(self, label: str, action_id: Optional[str] = None, event_id: Optional[str] = None, expected: Optional[str] = None, note: str = "") -> Dict[str, Any]:
        if not action_id and not event_id:
            raise RungatedError("action_id or event_id is required")
        body: Dict[str, Any] = {"label": label, "note": note}
        if action_id:
            body["action_id"] = action_id
        if event_id:
            body["event_id"] = event_id
        if expected:
            body["expected"] = expected
        return self._raise_for(self._client().post(self.gateway + "/v1/feedback", json=body, headers=self.headers()))

    def approval(self, approval_id: str) -> Dict[str, Any]:
        return self._raise_for(self._client().get(self.gateway + "/v1/approvals/" + approval_id, headers=self.headers()))

    def wrap_openai(self, client: Any) -> Any:
        return client.with_options(**self.llm_kwargs("openai"))

    def wrap_anthropic(self, client: Any) -> Any:
        return client.with_options(**self.llm_kwargs("anthropic"))

    def openai(self, **kw: Any) -> Any:
        from openai import OpenAI

        kw.setdefault("api_key", "rungated")
        return OpenAI(**{**kw, **self.llm_kwargs("openai")})

    def async_openai(self, **kw: Any) -> Any:
        from openai import AsyncOpenAI

        kw.setdefault("api_key", "rungated")
        return AsyncOpenAI(**{**kw, **self.llm_kwargs("openai")})

    def anthropic(self, **kw: Any) -> Any:
        from anthropic import Anthropic

        kw.setdefault("api_key", "rungated")
        return Anthropic(**{**kw, **self.llm_kwargs("anthropic")})

    def async_anthropic(self, **kw: Any) -> Any:
        from anthropic import AsyncAnthropic

        kw.setdefault("api_key", "rungated")
        return AsyncAnthropic(**{**kw, **self.llm_kwargs("anthropic")})

