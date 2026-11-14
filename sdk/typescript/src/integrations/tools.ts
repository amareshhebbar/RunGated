import type { Rungated } from "../client.js";
import { type ServerSpec, refuse, serverFor } from "../guard.js";

type Block = "raise" | "message";

async function guarded<T>(ab: Rungated, name: string, server: string | undefined, args: Record<string, unknown>, onBlock: Block, run: (args: Record<string, unknown>) => Promise<T>): Promise<T | string> {
  const d = await ab.checkTool(name, args, { server });
  if (!d.allowed) return refuse(d, onBlock);
  const out = await run(d.args ?? args);
  const r = await ab.checkResult(name, out, { server });
  if (!r.decision.allowed) return refuse(r.decision, onBlock);
  return r.result;
}

export function protectLangChainTools<T extends { name: string; invoke: (input: any, config?: any) => Promise<any> }>(ab: Rungated, tools: T[], opts: { server?: ServerSpec; onBlock?: Block } = {}): T[] {
  const onBlock = opts.onBlock ?? "message";
  return tools.map((tool) => {
    const proxy = Object.create(tool) as T;
    proxy.invoke = async (input: any, config?: any) => {
      const isCall = input && typeof input === "object" && input.type === "tool_call";
      const args: Record<string, unknown> = isCall ? input.args ?? {} : typeof input === "object" && input ? input : { input };
      const out = await guarded(ab, tool.name, serverFor(opts.server, tool.name), args, onBlock, async (a) => tool.invoke(isCall ? { ...input, args: a } : typeof input === "object" && input ? a : (a.input as any), config));
      return out;
    };
    return proxy;
  });
}

export function protectAiSdkTools<T extends Record<string, any>>(ab: Rungated, tools: T, opts: { server?: ServerSpec; onBlock?: Block } = {}): T {
  const onBlock = opts.onBlock ?? "message";
  const out: Record<string, unknown> = {};
  for (const [name, tool] of Object.entries(tools)) {
    if (typeof tool.execute !== "function") {
      out[name] = tool;
      continue;
    }
    const original = tool.execute.bind(tool);
    out[name] = { ...tool, execute: (args: any, options?: any) => guarded(ab, name, serverFor(opts.server, name), args ?? {}, onBlock, async (a) => original(a, options)) };
  }
  return out as T;
}

export function protectOpenAIAgentTools<T extends { name: string; invoke?: (runContext: any, input: string, details?: any) => Promise<any> }>(ab: Rungated, tools: T[], opts: { server?: ServerSpec; onBlock?: Block } = {}): T[] {
  const onBlock = opts.onBlock ?? "message";
  return tools.map((tool) => {
    if (typeof tool.invoke !== "function") return tool;
    const original = tool.invoke.bind(tool);
    const proxy = Object.create(tool) as T;
    (proxy as any).invoke = async (ctx: any, input: string, details?: any) => {
      let args: Record<string, unknown> = {};
      try {
        const parsed = input ? JSON.parse(input) : {};
        args = parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed : { value: parsed };
      } catch {
        args = { input };
      }
      return guarded(ab, tool.name, serverFor(opts.server, tool.name), args, onBlock, async (a) => original(ctx, JSON.stringify(a), details));
    };
    return proxy;
  });
}
