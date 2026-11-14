import type { Rungated } from "./client.js";
import { ActionBlocked, AgentContained, ApprovalDenied } from "./errors.js";
import type { Decision } from "./types.js";

export type ServerSpec = string | Record<string, string> | undefined;

export function serverFor(spec: ServerSpec, name: string): string | undefined {
  if (spec === undefined || typeof spec === "string") return spec;
  return spec[name];
}

export function refuse(d: Decision, onBlock: "raise" | "message"): string {
  if (onBlock === "message") return d.message || "Rungated blocked this action";
  if (d.verdict === "contain") throw new AgentContained(d);
  if (d.verdict === "hold") throw new ApprovalDenied(d);
  throw new ActionBlocked(d);
}

export interface ProtectOptions {
  name: string;
  server?: string;
  method?: string;
  onBlock?: "raise" | "message";
  scanResult?: boolean;
  toArgs?: (...a: any[]) => Record<string, unknown>;
  fromArgs?: (args: Record<string, unknown>, original: any[]) => any[];
}

export function protect<A extends any[], R>(ab: Rungated, fn: (...a: A) => R | Promise<R>, opts: ProtectOptions): (...a: A) => Promise<R | string> {
  const onBlock = opts.onBlock ?? "raise";
  const toArgs = opts.toArgs ?? ((...a: any[]) => (a.length === 1 && a[0] && typeof a[0] === "object" && !Array.isArray(a[0]) ? (a[0] as Record<string, unknown>) : { args: a }));
  const fromArgs = opts.fromArgs ?? ((args: Record<string, unknown>, original: any[]) => (original.length === 1 && original[0] && typeof original[0] === "object" && !Array.isArray(original[0]) ? [args] : (args.args as any[]) ?? original));
  return async (...a: A): Promise<R | string> => {
    const d = await ab.checkTool(opts.name, toArgs(...a), { server: opts.server, method: opts.method });
    if (!d.allowed) return refuse(d, onBlock);
    const callArgs = (d.args ? fromArgs(d.args, a) : a) as A;
    const out = await fn(...callArgs);
    if (opts.scanResult === false) return out;
    const r = await ab.checkResult(opts.name, out, { server: opts.server });
    if (!r.decision.allowed) return refuse(r.decision, onBlock);
    return r.result;
  };
}
