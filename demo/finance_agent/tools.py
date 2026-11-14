import base64
from typing import Any, Dict, List, Optional

import httpx

from .mcp_client import McpClient, McpError

TOOL_SCHEMAS: List[Dict[str, Any]] = [
    {"type": "function", "function": {"name": "fetch_url", "description": "Fetch a web page for research", "parameters": {"type": "object", "properties": {"url": {"type": "string"}}, "required": ["url"]}}},
    {"type": "function", "function": {"name": "get_invoice", "description": "Fetch one invoice by id", "parameters": {"type": "object", "properties": {"invoice_id": {"type": "string"}}, "required": ["invoice_id"]}}},
    {"type": "function", "function": {"name": "list_customers", "description": "List customers with balances", "parameters": {"type": "object", "properties": {"limit": {"type": "integer"}, "region": {"type": "string"}}}}},
    {"type": "function", "function": {"name": "send_email", "description": "Send an email to an internal recipient", "parameters": {"type": "object", "properties": {"to": {"type": "string"}, "subject": {"type": "string"}, "body": {"type": "string"}}, "required": ["to"]}}},
]


class Toolbox:
    def __init__(self, mcp: McpClient, egress_proxy: Optional[str] = None, egress_identity: Optional[str] = None):
        self.mcp = mcp
        self.mcp_ready = False
        self.http: httpx.Client
        if egress_proxy:
            token = base64.b64encode((egress_identity or "agent") .encode() + b":demo").decode()
            self.http = httpx.Client(timeout=20.0, proxy=httpx.Proxy(egress_proxy, headers={"Proxy-Authorization": "Basic " + token}))
        else:
            self.http = httpx.Client(timeout=20.0)

    def close(self) -> None:
        self.http.close()
        self.mcp.close()

    def run(self, name: str, args: Dict[str, Any]) -> Dict[str, Any]:
        if name == "fetch_url":
            return self._fetch(str(args.get("url", "")))
        return self._mcp(name, args)

    def _fetch(self, url: str) -> Dict[str, Any]:
        try:
            r = self.http.get(url)
        except httpx.HTTPError as e:
            return {"text": "request failed: %s" % e, "is_error": True}
        body = r.text[:4000]
        if r.status_code >= 400:
            return {"text": "HTTP %d: %s" % (r.status_code, body), "is_error": True}
        return {"text": body, "is_error": False}

    def _mcp(self, name: str, args: Dict[str, Any]) -> Dict[str, Any]:
        try:
            if not self.mcp_ready:
                self.mcp.initialize()
                self.mcp.list_tools()
                self.mcp_ready = True
            return self.mcp.call_tool(name, args)
        except McpError as e:
            return {"text": "ERROR: %s" % e, "is_error": True}
        except httpx.HTTPError as e:
            return {"text": "ERROR: %s" % e, "is_error": True}
