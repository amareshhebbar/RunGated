import os
from dataclasses import dataclass


@dataclass
class Endpoints:
    gateway: str = "http://127.0.0.1:8080"
    egress: str = "http://127.0.0.1:8081"
    mock_llm: str = "http://127.0.0.1:9300"
    mcp: str = "http://127.0.0.1:9301"
    web: str = "http://127.0.0.1:9302"
    agent_id: str = "finance-agent"
    mcp_server: str = "finance-mcp"
    tenant: str = "acme"
    model: str = "mock-1"

    @classmethod
    def from_env(cls) -> "Endpoints":
        d = cls()
        return cls(
            gateway=os.environ.get("DEMO_GATEWAY", d.gateway).rstrip("/"),
            egress=os.environ.get("DEMO_EGRESS", d.egress).rstrip("/"),
            mock_llm=os.environ.get("DEMO_MOCK_LLM", d.mock_llm).rstrip("/"),
            mcp=os.environ.get("DEMO_MCP", d.mcp).rstrip("/"),
            web=os.environ.get("DEMO_WEB", d.web).rstrip("/"),
            agent_id=os.environ.get("DEMO_AGENT", d.agent_id),
        )
