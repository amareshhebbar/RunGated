import base64
import json
import uuid
from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional, TypedDict

from langgraph.graph import END, START, StateGraph
from openai import OpenAI

from rungated import Rungated

from .config import Endpoints
from .mcp_client import McpClient
from .tools import TOOL_SCHEMAS, Toolbox

SYSTEM = "You are a finance assistant for Acme. Use the tools to answer questions about invoices and customers."


class State(TypedDict):
    messages: List[Dict[str, Any]]
    events: List[Dict[str, Any]]
    steps: int
    error: Optional[str]


@dataclass
class AgentResult:
    final: str
    events: List[Dict[str, Any]]
    error: Optional[str]
    session: str
    messages: List[Dict[str, Any]] = field(default_factory=list)


class FinanceAgent:
    def __init__(self, endpoints: Optional[Endpoints] = None, protected: bool = True, session: Optional[str] = None, max_steps: int = 6):
        self.ep = endpoints or Endpoints.from_env()
        self.protected = protected
        self.session = session or "run-" + uuid.uuid4().hex[:8]
        self.max_steps = max_steps
        if protected:
            self.ab = Rungated(gateway=self.ep.gateway, agent_id=self.ep.agent_id, session_id=self.session)
            self.llm = self.ab.openai()
            mcp = McpClient(self.ep.gateway + "/mcp/" + self.ep.mcp_server, headers=self.ab.headers())
            identity = "%s@%s" % (self.ep.agent_id, self.session)
            self.tools = Toolbox(mcp, egress_proxy=self.ep.egress, egress_identity=identity)
        else:
            self.ab = None
            self.llm = OpenAI(base_url=self.ep.mock_llm + "/v1", api_key="unused")
            self.tools = Toolbox(McpClient(self.ep.mcp + "/mcp"))
        self.graph = self._build()

    def close(self) -> None:
        self.tools.close()
        if self.ab is not None:
            self.ab.close()

    def _build(self):
        def model(state: State) -> Dict[str, Any]:
            try:
                resp = self.llm.chat.completions.create(model=self.ep.model, messages=state["messages"], tools=TOOL_SCHEMAS)
            except Exception as e:
                return {"error": "model call failed: %s" % e, "steps": state["steps"] + 1}
            m = resp.choices[0].message
            entry: Dict[str, Any] = {"role": "assistant", "content": m.content}
            if m.tool_calls:
                entry["tool_calls"] = [{"id": t.id, "type": "function", "function": {"name": t.function.name, "arguments": t.function.arguments}} for t in m.tool_calls]
            return {"messages": state["messages"] + [entry], "steps": state["steps"] + 1}

        def act(state: State) -> Dict[str, Any]:
            last = state["messages"][-1]
            msgs = list(state["messages"])
            events = list(state["events"])
            for tc in last.get("tool_calls", []):
                name = tc["function"]["name"]
                try:
                    args = json.loads(tc["function"]["arguments"] or "{}")
                except ValueError:
                    args = {}
                out = self.tools.run(name, args)
                events.append({"tool": name, "args": args, "result": out["text"][:400], "is_error": out["is_error"]})
                msgs.append({"role": "tool", "tool_call_id": tc["id"], "content": out["text"]})
            return {"messages": msgs, "events": events}

        def route(state: State) -> str:
            if state.get("error") or state["steps"] >= self.max_steps:
                return END
            last = state["messages"][-1]
            return "act" if last.get("role") == "assistant" and last.get("tool_calls") else END

        g = StateGraph(State)
        g.add_node("model", model)
        g.add_node("act", act)
        g.add_edge(START, "model")
        g.add_conditional_edges("model", route, {"act": "act", END: END})
        g.add_edge("act", "model")
        return g.compile()

    def run(self, prompt: str) -> AgentResult:
        state: State = {"messages": [{"role": "system", "content": SYSTEM}, {"role": "user", "content": prompt}], "events": [], "steps": 0, "error": None}
        out = self.graph.invoke(state, {"recursion_limit": 40})
        final = ""
        for m in reversed(out["messages"]):
            if m.get("role") == "assistant" and m.get("content"):
                final = m["content"]
                break
        return AgentResult(final=final, events=out["events"], error=out.get("error"), session=self.session, messages=out["messages"])
