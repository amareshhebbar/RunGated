import type { Decision } from "./types.js";

export class RungatedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = new.target.name;
  }
}

export class GatewayError extends RungatedError {
  readonly status: number;
  readonly body: unknown;
  constructor(message: string, status = 0, body?: unknown) {
    super(message);
    this.status = status;
    this.body = body;
  }
}

export class ActionBlocked extends RungatedError {
  readonly decision: Decision;
  constructor(decision: Decision) {
    super(decision.message || "Rungated blocked this action");
    this.decision = decision;
  }
  get verdict(): string {
    return this.decision.verdict;
  }
  get reasons(): string[] {
    return this.decision.reasons;
  }
}

export class ApprovalDenied extends ActionBlocked {
  get approvalId(): string | undefined {
    return this.decision.approvalId;
  }
}

export class AgentContained extends ActionBlocked {
  get incidentId(): string | undefined {
    return this.decision.incidentId;
  }
}
