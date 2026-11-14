const KEY = "rungated_api_key";
const BASE = "rungated_base_url";
const THEME = "rungated_theme";

function read(k: string): string {
  try {
    return localStorage.getItem(k) ?? "";
  } catch {
    return "";
  }
}

function write(k: string, v: string) {
  try {
    if (v) localStorage.setItem(k, v);
    else localStorage.removeItem(k);
  } catch {}
}

export const getKey = () => read(KEY);
export const setKey = (v: string) => write(KEY, v.trim());
export const getBase = () => read(BASE).replace(/\/+$/, "");
export const setBase = (v: string) => write(BASE, v.trim());
export const getTheme = (): "light" | "dark" => (read(THEME) === "light" ? "light" : "dark");
export const setTheme = (v: "light" | "dark") => write(THEME, v);

export class ApiError extends Error {
  status: number;
  body: unknown;
  constructor(status: number, message: string, body: unknown) {
    super(message);
    this.status = status;
    this.body = body;
  }
}

export function headers(extra?: Record<string, string>): Record<string, string> {
  const h: Record<string, string> = { ...(extra ?? {}) };
  const k = getKey();
  if (k) h["Authorization"] = "Bearer " + k;
  return h;
}

export async function api<T>(path: string, init?: { method?: string; body?: unknown; raw?: string }): Promise<T> {
  const hasBody = init?.body !== undefined || init?.raw !== undefined;
  const res = await fetch(getBase() + path, {
    method: init?.method ?? "GET",
    headers: headers(hasBody ? { "Content-Type": init?.raw !== undefined ? "text/plain" : "application/json" } : undefined),
    body: init?.raw !== undefined ? init.raw : init?.body !== undefined ? JSON.stringify(init.body) : undefined,
    cache: "no-store",
  });
  const text = await res.text();
  let parsed: unknown = null;
  try {
    parsed = text ? JSON.parse(text) : null;
  } catch {
    parsed = text;
  }
  if (!res.ok) {
    const msg = (parsed && typeof parsed === "object" && "error" in parsed ? String((parsed as { error: unknown }).error) : "") || res.statusText || "request failed";
    throw new ApiError(res.status, msg, parsed);
  }
  return parsed as T;
}

export interface StreamHandle {
  close: () => void;
}

export function openStream(path: string, onEvent: (name: string, data: string) => void, onState?: (s: "open" | "closed") => void): StreamHandle {
  let stopped = false;
  let ctl: AbortController | null = null;

  const run = async () => {
    let delay = 1000;
    while (!stopped) {
      ctl = new AbortController();
      try {
        const res = await fetch(getBase() + path, { headers: headers({ Accept: "text/event-stream" }), signal: ctl.signal, cache: "no-store" });
        if (!res.ok || !res.body) throw new Error("stream " + res.status);
        onState?.("open");
        delay = 1000;
        const reader = res.body.getReader();
        const dec = new TextDecoder();
        let buf = "";
        let name = "";
        let data: string[] = [];
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += dec.decode(value, { stream: true });
          let idx: number;
          while ((idx = buf.indexOf("\n")) >= 0) {
            const line = buf.slice(0, idx).replace(/\r$/, "");
            buf = buf.slice(idx + 1);
            if (line === "") {
              if (data.length) onEvent(name || "message", data.join("\n"));
              name = "";
              data = [];
            } else if (line.startsWith(":")) {
              continue;
            } else if (line.startsWith("event:")) {
              name = line.slice(6).trim();
            } else if (line.startsWith("data:")) {
              data.push(line.slice(5).replace(/^ /, ""));
            }
          }
        }
      } catch {}
      onState?.("closed");
      if (stopped) return;
      await new Promise((r) => setTimeout(r, delay));
      delay = Math.min(delay * 2, 15000);
    }
  };
  run();
  return {
    close: () => {
      stopped = true;
      ctl?.abort();
    },
  };
}
