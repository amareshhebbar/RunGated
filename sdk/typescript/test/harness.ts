import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import http from "node:http";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
export const ROOT = path.resolve(here, "..", "..", "..");

const TOK = /\[\[(?:PII|PCI|PHI|SECRET|DATA|FINANCIAL)_[A-Z0-9_]{1,40}_[0-9A-F]{8}\]\]/;

export function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.listen(0, "127.0.0.1", () => {
      const port = (s.address() as net.AddressInfo).port;
      s.close(() => resolve(port));
    });
    s.on("error", reject);
  });
}

export interface Upstream {
  url: string;
  seen: { path: string; body: string }[];
  close: () => Promise<void>;
}

export async function startUpstream(): Promise<Upstream> {
  const seen: { path: string; body: string }[] = [];
  const server = http.createServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => {
      const raw = Buffer.concat(chunks).toString();
      seen.push({ path: req.url ?? "", body: raw });
      const tok = (raw.match(TOK) ?? [""])[0];
      const send = (o: unknown) => {
        const b = JSON.stringify(o);
        res.writeHead(200, { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(b) });
        res.end(b);
      };
      if ((req.url ?? "").endsWith("/chat/completions")) {
        return send({
          id: "c1",
          object: "chat.completion",
          created: 1,
          model: "mock-1",
          choices: [
            {
              index: 0,
              finish_reason: "tool_calls",
              message: {
                role: "assistant",
                content: "Emailing " + tok,
                tool_calls: [
                  { id: "call_1", type: "function", function: { name: "get_invoice", arguments: '{"invoice_id":"INV-000123"}' } },
                  { id: "call_2", type: "function", function: { name: "wipe_database", arguments: "{}" } },
                ],
              },
            },
          ],
          usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
        });
      }
      if ((req.url ?? "").endsWith("/messages")) {
        return send({
          id: "msg_1",
          type: "message",
          role: "assistant",
          model: "claude-sonnet-4",
          content: [
            { type: "text", text: "Emailing " + tok },
            { type: "tool_use", id: "tu_1", name: "get_invoice", input: { invoice_id: "INV-000123" } },
            { type: "tool_use", id: "tu_2", name: "wipe_database", input: {} },
          ],
          stop_reason: "tool_use",
          stop_sequence: null,
          usage: { input_tokens: 10, output_tokens: 5 },
        });
      }
      res.writeHead(404);
      res.end();
    });
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  const port = (server.address() as net.AddressInfo).port;
  return { url: "http://127.0.0.1:" + port, seen, close: () => new Promise((r) => server.close(() => r())) };
}

export interface Gateway {
  url: string;
  upstream: Upstream;
  stop: () => Promise<void>;
}

export function buildBinary(): string {
  const exe = path.join(ROOT, "bin", "rungated");
  execFileSync("go", ["build", "-o", exe, "./cmd/rungated"], { cwd: ROOT, stdio: "inherit" });
  return exe;
}

export async function startGateway(exe: string): Promise<Gateway> {
  const upstream = await startUpstream();
  const port = await freePort();
  const data = mkdtempSync(path.join(os.tmpdir(), "ab-ts-"));
  const proc: ChildProcess = spawn(exe, ["serve", "--dev", "--listen", "127.0.0.1:" + port, "--data", data], {
    cwd: ROOT,
    env: { ...process.env, RUNGATED_OPENAI_BASE_URL: upstream.url + "/v1", RUNGATED_ANTHROPIC_BASE_URL: upstream.url + "/v1", OPENAI_API_KEY: "sk-up", ANTHROPIC_API_KEY: "ak-up" },
    stdio: "ignore",
  });
  const url = "http://127.0.0.1:" + port;
  let ok = false;
  for (let i = 0; i < 150 && !ok; i++) {
    try {
      const r = await fetch(url + "/v1/health");
      ok = r.ok;
    } catch {
      await new Promise((r) => setTimeout(r, 100));
    }
  }
  if (!ok) {
    proc.kill();
    throw new Error("gateway did not start");
  }
  return {
    url,
    upstream,
    stop: async () => {
      proc.kill("SIGTERM");
      await new Promise((r) => proc.once("exit", r));
      await upstream.close();
      rmSync(data, { recursive: true, force: true });
    },
  };
}

export function financeManifestWithTransfer(): string {
  let text = readFileSync(path.join(ROOT, "spec", "ats", "examples", "finance-agent.yaml"), "utf8");
  text = text.replace("id: finance-agent", "id: sdk-approver");
  const block = "    - name: transfer\n      server: finance-mcp\n      blastRadius: high\n      requireApproval: true\n      args:\n        amount: {type: number, required: true, max: 5000}\n";
  return text.replace("    - name: send_email\n", block + "    - name: send_email\n");
}
