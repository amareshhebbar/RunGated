export type Verdict = "allow" | "warn" | "redact" | "hold" | "block" | "contain";

export const VERDICTS: Verdict[] = ["allow", "warn", "redact", "hold", "block", "contain"];

export const MODES = ["observe", "recommend", "human", "auto_contain", "full_auto"] as const;

export interface Finding {
  detector: string;
  tier: number;
  category: string;
  rule?: string;
  severity: number;
  confidence: number;
  message: string;
  data_class?: string;
  span?: { part: number; start: number; end: number };
  redactable?: boolean;
  min_verdict?: Verdict;
}

export interface Redaction {
  part: number;
  start: number;
  end: number;
  token: string;
  class: string;
}

export interface Decision {
  action_id: string;
  score: number;
  computed: Verdict;
  verdict: Verdict;
  mode: string;
  findings: Finding[] | null;
  policy_allowed: boolean;
  policy_reasons?: string[];
  reasons?: string[];
  approval_id?: string;
  approval_status?: string;
  redactions?: Redaction[];
  latency_us: number;
  fail_mode?: string;
  degraded?: boolean;
  judge_reason?: string;
  incident_id?: string;
}

export interface TextPart {
  source: string;
  text: string;
  path?: string;
}

export interface Action {
  id: string;
  tenant_id: string;
  agent_id: string;
  session_id: string;
  channel: string;
  kind: string;
  provider?: string;
  model?: string;
  server?: string;
  tool?: string;
  method?: string;
  args?: Record<string, unknown>;
  host?: string;
  url?: string;
  texts?: TextPart[];
  bytes?: number;
  tokens?: number;
  cost_usd?: number;
  meta?: Record<string, string>;
  time: string;
}

export interface RungatedEvent {
  id: string;
  seq?: number;
  time: string;
  tenant_id: string;
  agent_id: string;
  session_id: string;
  type: string;
  action?: Action;
  decision?: Decision;
  message?: string;
  data?: Record<string, unknown>;
}

export interface Agent {
  id: string;
  name: string;
  tenant: string;
  owner: string;
  purpose: string;
  risk_tier: string;
  mode: string;
  mode_override?: string;
  effective_mode: string;
  status: string;
  version: number;
  tools: number;
  mcp_servers: number;
  created_at: string;
  updated_at: string;
  last_seen?: string;
}

export interface AgentDetail {
  agent: Agent;
  manifest: string;
  cedar: string;
  usage: Record<string, number>;
}

export interface Approval {
  id: string;
  tenant: string;
  agent_id: string;
  session_id: string;
  kind: string;
  summary: string;
  action?: Action;
  decision?: Decision;
  status: string;
  reason?: string;
  decided_by?: string;
  created_at: string;
  expires_at: string;
  decided_at?: string;
  on_timeout: string;
}

export interface Incident {
  id: string;
  tenant: string;
  agent_id: string;
  session_id: string;
  title: string;
  severity: number;
  verdict: string;
  status: string;
  categories: string[];
  event_ids: string[];
  snapshot_ref?: string;
  created_at: string;
  updated_at: string;
  notes?: string[];
}

export interface Pin {
  tenant: string;
  server: string;
  tool: string;
  fingerprint: string;
  description: string;
  schema?: unknown;
  status: string;
  first_seen: string;
  approved_by?: string;
  pending_fingerprint?: string;
  pending_description?: string;
  pending_schema?: unknown;
  pending_since?: string;
  approval_id?: string;
}

export interface Containment {
  id: string;
  tenant: string;
  agent_id: string;
  session_id?: string;
  reason: string;
  by: string;
  automatic: boolean;
  created_at: string;
  released_at?: string;
  snapshot_ref?: string;
}

export interface Stats {
  decisions: {
    total: number;
    verdicts: Record<string, number>;
    computed: Record<string, number>;
    kinds: Record<string, number>;
    categories: Record<string, number>;
    degraded: number;
    p50_us: number;
    p95_us: number;
    p99_us: number;
    max_us: number;
    samples: number;
    uptime_s: number;
  };
  vault: Record<string, number>;
  active_containment: number;
  pending_approvals: number;
  agents: number;
}

export interface AuditRecord {
  seq: number;
  time: string;
  event_id: string;
  tenant: string;
  agent_id: string;
  type: string;
  hash?: string;
  prev_hash?: string;
}

export interface VerifyResult {
  ok: boolean;
  records: number;
  first_bad_seq?: number;
  error?: string;
  head_hash?: string;
  events_checked: number;
}

export interface PolicyRecord {
  tenant: string;
  source: string;
  version: number;
  author: string;
  updated_at: string;
}
