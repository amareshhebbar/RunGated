export type Verdict = "allow" | "warn" | "redact" | "hold" | "block" | "contain";

export interface Finding {
  category: string;
  confidence: number;
  data_class?: string;
  detector?: string;
  message?: string;
  rule?: string;
  severity?: number;
  tier?: number;
}

export interface Redaction {
  part: number;
  start: number;
  end: number;
  token: string;
  class: string;
}

export interface Decision {
  verdict: Verdict | string;
  computed: string;
  allowed: boolean;
  blocked: boolean;
  score: number;
  mode: string;
  reasons: string[];
  findings: Finding[];
  actionId: string;
  approvalId?: string;
  approvalStatus?: string;
  incidentId?: string;
  redactedTexts?: string[];
  redactions: Redaction[];
  message: string;
  args?: Record<string, unknown>;
  latencyUs: number;
  raw: Record<string, unknown>;
}

export type TextSource = "system" | "user" | "assistant" | "tool_result" | "tool_args" | "web" | "file" | "rag" | "tool_description" | "egress_body";

export interface TextPart {
  source: TextSource | string;
  text: string;
  path?: string;
}

export type Kind = "llm.request" | "llm.response" | "tool.call" | "tool.result" | "mcp.tools_list" | "mcp.resource" | "mcp.prompt" | "egress.request" | "egress.response";

export interface DecideOptions {
  tool?: string;
  server?: string;
  method?: string;
  args?: Record<string, unknown>;
  texts?: TextPart[];
  model?: string;
  provider?: string;
  host?: string;
  url?: string;
  bytes?: number;
  meta?: Record<string, string>;
  channel?: string;
  sessionId?: string;
  wait?: boolean;
}

export interface RungatedOptions {
  gateway?: string;
  agentId?: string;
  agentKey?: string;
  token?: string;
  apiKey?: string;
  sessionId?: string;
  timeoutMs?: number;
  holdTimeoutMs?: number;
  failOpen?: boolean;
  wait?: boolean;
  fetch?: typeof fetch;
}

export interface LlmClientOptions {
  baseURL: string;
  apiKey: string;
  defaultHeaders: Record<string, string>;
}
