from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional


@dataclass
class Decision:
    verdict: str
    computed: str = ""
    allowed: bool = True
    score: int = 0
    mode: str = ""
    reasons: List[str] = field(default_factory=list)
    findings: List[Dict[str, Any]] = field(default_factory=list)
    action_id: str = ""
    approval_id: Optional[str] = None
    approval_status: Optional[str] = None
    incident_id: Optional[str] = None
    redacted_texts: Optional[List[str]] = None
    redactions: List[Dict[str, Any]] = field(default_factory=list)
    message: str = ""
    args: Optional[Dict[str, Any]] = None
    latency_us: int = 0
    raw: Dict[str, Any] = field(default_factory=dict)

    @property
    def blocked(self) -> bool:
        return not self.allowed

    @classmethod
    def from_json(cls, d: Dict[str, Any]) -> "Decision":
        return cls(
            verdict=d.get("verdict", ""),
            computed=d.get("computed", ""),
            allowed=bool(d.get("allowed", False)),
            score=int(d.get("score", 0)),
            mode=d.get("mode", ""),
            reasons=list(d.get("reasons") or []),
            findings=list(d.get("findings") or []),
            action_id=d.get("action_id", ""),
            approval_id=d.get("approval_id"),
            approval_status=d.get("approval_status"),
            incident_id=d.get("incident_id"),
            redacted_texts=d.get("redacted_texts"),
            redactions=list(d.get("redactions") or []),
            message=d.get("message", ""),
            args=d.get("args"),
            latency_us=int(d.get("latency_us", 0)),
            raw=d,
        )

    @classmethod
    def open_allow(cls, reason: str) -> "Decision":
        return cls(verdict="allow", computed="allow", allowed=True, reasons=[reason])
