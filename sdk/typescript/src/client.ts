import { ActionBlocked, AgentContained, RungatedError, ApprovalDenied, GatewayError } from "./errors.js";
import { collectStrings, replaceStrings } from "./strings.js";
import type { RungatedOptions, Decision, DecideOptions, Kind, LlmClientOptions } from "./types.js";

const DEFAULT_GATEWAY = "http://127.0.0.1:8080";

function env(name: string): string | undefined {
  const p = (globalThis as any).process;
  const v = p?.env?.[name];
  return v ? String(v) : undefined;
}

export function toDecision(d: Record<string, any>): Decision {
  const allowed = Boolean(d.allowed);
  return {
    verdict: d.verdict ?? "",
    computed: d.computed ?? "",
    allowed,
    blocked: !allowed,
    score: Number(d.score ?? 0),
    mode: d.mode ?? "",
    reasons: d.reasons ?? [],
    findings: d.findings ?? [],
    actionId: d.action_id ?? "",
    approvalId: d.approval_id ?? undefined,
    approvalStatus: d.approval_status ?? undefined,
    incidentId: d.incident_id ?? undefined,
    redactedTexts: d.redacted_texts ?? undefined,
    redactions: d.redactions ?? [],
    message: d.message ?? "",
    args: d.args ?? undefined,
    latencyUs: Number(d.latency_us ?? 0),
    raw: d,
  };
}

export function enforce(d: Decision): Decision {
  if (d.allowed) return d;
  if (d.verdict === "contain") throw new AgentContained(d);
  if (d.verdict === "hold") throw new ApprovalDenied(d);
  throw new ActionBlocked(d);
}

export class Rungated {
  readonly gateway: string;
  readonly agentId?: string;
  readonly agentKey?: string;
  readonly token?: string;
  readonly apiKey?: string;
  readonly sessionId?: string;
  readonly timeoutMs: number;
  readonly holdTimeoutMs: number;
  readonly failOpen: boolean;
  readonly wait: boolean;
  private readonly fetchImpl: typeof fetch;

  constructor(opts: RungatedOptions = {}) {
    this.gateway = (opts.gateway ?? env("RUNGATED_GATEWAY") ?? DEFAULT_GATEWAY).replace(/\/+$/, "");
    this.agentId = opts.agentId ?? env("RUNGATED_AGENT");
    this.agentKey = opts.agentKey ?? env("RUNGATED_AGENT_KEY");
    this.token = opts.token ?? env("RUNGATED_TOKEN");
    this.apiKey = opts.apiKey ?? env("RUNGATED_API_KEY");
    this.sessionId = opts.sessionId ?? env("RUNGATED_SESSION");
    this.timeoutMs = opts.timeoutMs ?? 30_000;
    this.holdTimeoutMs = opts.holdTimeoutMs ?? 600_000;
    this.failOpen = opts.failOpen ?? false;
    this.wait = opts.wait ?? true;
    this.fetchImpl = opts.fetch ?? ((...a: Parameters<typeof fetch>) => fetch(...a));
  }

  withSession(sessionId: string): Rungated {
    return new Rungated({
      gateway: this.gateway,
      agentId: this.agentId,
      agentKey: this.agentKey,
      token: this.token,
      apiKey: this.apiKey,
      sessionId,
      timeoutMs: this.timeoutMs,
      holdTimeoutMs: this.holdTimeoutMs,
      failOpen: this.failOpen,
      wait: this.wait,
      fetch: this.fetchImpl,
    });
  }

  headers(): Record<string, string> {
    const h: Record<string, string> = {};
    if (this.token) h["X-Rungated-Token"] = this.token;
    else {
      if (this.agentId) h["X-Rungated-Agent"] = this.agentId;
      if (this.agentKey) h["X-Rungated-Key"] = this.agentKey;
    }
    if (this.apiKey) h["Authorization"] = "Bearer " + this.apiKey;
    if (this.sessionId) h["X-Rungated-Session"] = this.sessionId;
    return h;
  }

  llmUrl(provider: "openai" | "anthropic"): string {
    if (provider === "openai") return this.gateway + "/llm/openai/v1";
    if (provider === "anthropic") return this.gateway + "/llm/anthropic";
    throw new RungatedError("provider must be openai or anthropic");
  }

  llmOptions(provider: "openai" | "anthropic"): LlmClientOptions {
    return { baseURL: this.llmUrl(provider), apiKey: "rungated", defaultHeaders: this.headers() };
  }

  private async request(method: string, path: string, init: { body?: BodyInit; json?: unknown; query?: Record<string, string>; headers?: Record<string, string>; timeoutMs?: number } = {}): Promise<any> {
    const url = new URL(this.gateway + path);
    for (const [k, v] of Object.entries(init.query ?? {})) url.searchParams.set(k, v);
    const headers: Record<string, string> = { ...this.headers(), ...(init.headers ?? {}) };
    let body = init.body;
    if (init.json !== undefined) {
      body = JSON.stringify(init.json);
      headers["Content-Type"] = "application/json";
    }
    const res = await this.fetchImpl(url.toString(), { method, headers, body, signal: AbortSignal.timeout(init.timeoutMs ?? this.timeoutMs) });
    const text = await res.text();
    let data: any = text;
    try {
      data = text ? JSON.parse(text) : {};
    } catch {
      data = text;
    }
    if (res.status >= 400) {
      throw new GatewayError((data && data.error) || "gateway returned HTTP " + res.status, res.status, data);
    }
    return data;
  }

  private openAllow(e: unknown): Decision {
    if (this.failOpen) {
      return toDecision({ verdict: "allow", computed: "allow", allowed: true, reasons: ["gateway unreachable, failing open: " + String(e)] });
    }
    throw new GatewayError("gateway unreachable: " + String(e));
  }

  async decide(kind: Kind, opts: DecideOptions = {}): Promise<Decision> {
    const { wait, sessionId, ...rest } = opts;
    const body: Record<string, unknown> = { kind };
    const map: Record<string, string> = { sessionId: "session_id" };
    for (const [k, v] of Object.entries(rest)) {
      if (v === undefined || v === null || v === "") continue;
      body[map[k] ?? k] = v;
    }
    if (this.agentId) body.agent_id = this.agentId;
    const sid = sessionId ?? this.sessionId;
    if (sid) body.session_id = sid;
    try {
      const data = await this.request("POST", "/v1/decide", { json: body, query: (wait ?? this.wait) ? { wait: "true" } : undefined, timeoutMs: this.holdTimeoutMs });
      return toDecision(data);
    } catch (e) {
      if (e instanceof GatewayError && e.status > 0) throw e;
      return this.openAllow(e);
    }
  }

  async checkTool(tool: string, args?: Record<string, unknown>, opts: Omit<DecideOptions, "tool" | "args"> & { enforce?: boolean } = {}): Promise<Decision> {
    const { enforce: doEnforce, ...rest } = opts;
    const d = await this.decide("tool.call", { ...rest, tool, args });
    return doEnforce ? enforce(d) : d;
  }

  async checkResult<T>(tool: string, result: T, opts: { server?: string; enforce?: boolean } = {}): Promise<{ decision: Decision; result: T }> {
    const strings = collectStrings(result);
    if (strings.length === 0) {
      return { decision: toDecision({ verdict: "allow", computed: "allow", allowed: true, reasons: ["no text in result"] }), result };
    }
    const texts = strings.map((s) => ({ source: "tool_result", text: s.text, path: s.path.join(".") }));
    const decision = await this.decide("tool.result", { tool, server: opts.server, texts });
    if (opts.enforce) enforce(decision);
    if (decision.allowed && decision.redactedTexts && decision.redactedTexts.length === strings.length) {
      return { decision, result: replaceStrings(result, strings, decision.redactedTexts) };
    }
    return { decision, result };
  }

  async scan(text: string, opts: Omit<DecideOptions, "texts"> & { source?: string; kind?: Kind; enforce?: boolean } = {}): Promise<Decision> {
    const { source, kind, enforce: doEnforce, ...rest } = opts;
    const d = await this.decide(kind ?? "llm.request", { ...rest, texts: [{ source: source ?? "user", text }] });
    return doEnforce ? enforce(d) : d;
  }

  async restore(value: string): Promise<string>;
  async restore<T extends object>(value: T): Promise<T>;
  async restore(value: unknown): Promise<unknown> {
    const body: Record<string, unknown> = { agent_id: this.agentId, session_id: this.sessionId };
    if (typeof value === "string") body.text = value;
    else body.value = value;
    const data = await this.request("POST", "/v1/restore", { json: body });
    return typeof value === "string" ? data.text : data.value;
  }

  async register(manifest: string, rotateKey = false): Promise<any> {
    return this.request("POST", "/v1/agents", { body: manifest, query: rotateKey ? { rotate_key: "true" } : undefined, headers: { "Content-Type": "application/yaml" } });
  }

  async issueToken(sessionId?: string, ttlSeconds = 0): Promise<{ token: string; agent_id: string; session_id: string; expires_at: string; token_id: string }> {
    if (!this.agentId) throw new RungatedError("agentId is required");
    return this.request("POST", "/v1/agents/" + encodeURIComponent(this.agentId) + "/token", { json: { session_id: sessionId ?? this.sessionId ?? "", ttl_seconds: ttlSeconds } });
  }

  async health(): Promise<any> {
    return this.request("GET", "/v1/health");
  }

  async feedback(label: "correct" | "false_positive" | "false_negative", ref: { actionId?: string; eventId?: string }, opts: { expected?: string; note?: string } = {}): Promise<any> {
    if (!ref.actionId && !ref.eventId) throw new RungatedError("actionId or eventId is required");
    return this.request("POST", "/v1/feedback", { json: { label, action_id: ref.actionId, event_id: ref.eventId, expected: opts.expected, note: opts.note ?? "" } });
  }

  async approval(id: string): Promise<any> {
    return this.request("GET", "/v1/approvals/" + encodeURIComponent(id));
  }
}
