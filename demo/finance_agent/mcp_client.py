import json
from typing import Any, Dict, List, Optional

import httpx


class McpError(Exception):
    pass


class McpClient:
    def __init__(self, url: str, headers: Optional[Dict[str, str]] = None, timeout: float = 30.0):
        self.url = url
        self.headers = dict(headers or {})
        self.session_id: Optional[str] = None
        self._id = 0
        self._http = httpx.Client(timeout=timeout)

    def close(self) -> None:
        self._http.close()

    def _post(self, payload: Dict[str, Any]) -> Optional[Dict[str, Any]]:
        h = {**self.headers, "Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
        if self.session_id:
            h["Mcp-Session-Id"] = self.session_id
        r = self._http.post(self.url, json=payload, headers=h)
        sid = r.headers.get("Mcp-Session-Id")
        if sid:
            self.session_id = sid
        if r.status_code == 202 or not r.content:
            return None
        if r.status_code >= 400:
            try:
                body = r.json()
            except ValueError:
                body = {"error": r.text}
            raise McpError("HTTP %d: %s" % (r.status_code, json.dumps(body)[:300]))
        ctype = r.headers.get("Content-Type", "")
        if "text/event-stream" in ctype:
            for line in r.text.splitlines():
                if line.startswith("data:"):
                    data = line[5:].strip()
                    if data:
                        msg = json.loads(data)
                        if "id" in msg:
                            return msg
            return None
        return r.json()

    def request(self, method: str, params: Optional[Dict[str, Any]] = None) -> Any:
        self._id += 1
        msg = self._post({"jsonrpc": "2.0", "id": self._id, "method": method, "params": params or {}})
        if msg is None:
            return None
        if "error" in msg:
            raise McpError(msg["error"].get("message", "mcp error"))
        return msg.get("result")

    def initialize(self) -> None:
        self.request("initialize", {"protocolVersion": "2025-03-26", "capabilities": {}, "clientInfo": {"name": "finance-agent", "version": "1.0"}})
        self._post({"jsonrpc": "2.0", "method": "notifications/initialized"})

    def list_tools(self) -> List[Dict[str, Any]]:
        res = self.request("tools/list") or {}
        return res.get("tools", [])

    def call_tool(self, name: str, arguments: Dict[str, Any]) -> Dict[str, Any]:
        res = self.request("tools/call", {"name": name, "arguments": arguments}) or {}
        text = "".join(c.get("text", "") for c in res.get("content", []) if isinstance(c, dict))
        return {"text": text, "is_error": bool(res.get("isError"))}
