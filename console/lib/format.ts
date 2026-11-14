import type { Verdict } from "./types";

export function ago(iso?: string, now = Date.now()): string {
  if (!iso) return "never";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "unknown";
  const s = Math.max(0, Math.round((now - t) / 1000));
  if (s < 5) return "just now";
  if (s < 60) return s + "s ago";
  if (s < 3600) return Math.floor(s / 60) + "m ago";
  if (s < 86400) return Math.floor(s / 3600) + "h ago";
  return Math.floor(s / 86400) + "d ago";
}

export function clock(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleTimeString([], { hour12: false }) + "." + String(d.getMilliseconds()).padStart(3, "0");
}

export function stamp(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString([], { hour12: false });
}

export function micros(us: number): string {
  if (!us) return "0";
  if (us < 1000) return us + " us";
  if (us < 1_000_000) return (us / 1000).toFixed(us < 10_000 ? 1 : 0) + " ms";
  return (us / 1_000_000).toFixed(2) + " s";
}

export function num(n: number | undefined): string {
  return (n ?? 0).toLocaleString();
}

export function countdown(expires: string, now: number): string {
  const s = Math.floor((Date.parse(expires) - now) / 1000);
  if (Number.isNaN(s)) return "";
  if (s <= 0) return "expired";
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const r = s % 60;
  if (h > 0) return h + "h " + m + "m";
  return m + ":" + String(r).padStart(2, "0");
}

export const verdictRank: Record<string, number> = { allow: 0, warn: 1, redact: 2, hold: 3, block: 4, contain: 5 };

export function atLeast(v: string | undefined, min: string): boolean {
  return (verdictRank[v ?? "allow"] ?? 0) >= (verdictRank[min] ?? 0);
}

export const verdictText: Record<Verdict, string> = {
  allow: "text-allow",
  warn: "text-warn",
  redact: "text-redact",
  hold: "text-hold",
  block: "text-block",
  contain: "text-contain",
};

export const verdictBg: Record<Verdict, string> = {
  allow: "bg-allow",
  warn: "bg-warn",
  redact: "bg-redact",
  hold: "bg-hold",
  block: "bg-block",
  contain: "bg-contain",
};

export function summarize(a?: { kind: string; tool?: string; server?: string; host?: string; method?: string; url?: string; model?: string }): string {
  if (!a) return "";
  if (a.tool) return a.kind + "  " + (a.server ? a.server + "/" : "") + a.tool;
  if (a.host) return a.kind + "  " + (a.method ?? "") + " " + a.host;
  if (a.model) return a.kind + "  " + a.model;
  return a.kind;
}
