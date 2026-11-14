from __future__ import annotations

from typing import Any, Optional


class RungatedError(Exception):
    pass


class GatewayError(RungatedError):
    def __init__(self, message: str, status: int = 0, body: Any = None):
        super().__init__(message)
        self.status = status
        self.body = body


class ActionBlocked(RungatedError):
    def __init__(self, decision: Any):
        self.decision = decision
        super().__init__(decision.message or "Rungated blocked this action")

    @property
    def verdict(self) -> str:
        return self.decision.verdict

    @property
    def reasons(self) -> list:
        return self.decision.reasons


class ApprovalDenied(ActionBlocked):
    @property
    def approval_id(self) -> Optional[str]:
        return self.decision.approval_id


class AgentContained(ActionBlocked):
    @property
    def incident_id(self) -> Optional[str]:
        return self.decision.incident_id
