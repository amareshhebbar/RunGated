"use client";

import { ReactNode, useEffect } from "react";
import type { Verdict } from "@/lib/types";
import { verdictBg, verdictText } from "@/lib/format";

export function VerdictBadge({ verdict, small }: { verdict?: string; small?: boolean }) {
  const v = (verdict ?? "allow") as Verdict;
  const text = verdictText[v] ?? "text-muted";
  const bg = verdictBg[v] ?? "bg-muted";
  return (
    <span className={"inline-flex items-center gap-1.5 rounded border border-line bg-raised font-mono uppercase " + text + (small ? " px-1.5 py-0 text-[10px]" : " px-2 py-0.5 text-[11px]")}>
      <span className={"h-1.5 w-1.5 rounded-full " + bg} />
      {v}
    </span>
  );
}

export function Chip({ children, tone }: { children: ReactNode; tone?: "muted" | "accent" | "warn" | "block" | "allow" }) {
  const t = tone === "accent" ? "text-accent" : tone === "warn" ? "text-warn" : tone === "block" ? "text-block" : tone === "allow" ? "text-allow" : "text-muted";
  return <span className={"inline-block rounded border border-line bg-raised px-1.5 py-0 font-mono text-[10px] " + t}>{children}</span>;
}

export function Card({ title, right, children, className }: { title?: ReactNode; right?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={"card " + (className ?? "")}>
      {(title || right) && (
        <header className="flex items-center justify-between border-b border-line px-3 py-2">
          <h2 className="text-xs font-semibold uppercase tracking-wide text-muted">{title}</h2>
          <div className="flex items-center gap-2">{right}</div>
        </header>
      )}
      {children}
    </section>
  );
}

export function Stat({ label, value, sub, tone }: { label: string; value: ReactNode; sub?: ReactNode; tone?: string }) {
  return (
    <div className="card px-3 py-2.5">
      <div className="label">{label}</div>
      <div className={"mt-1 font-mono text-2xl font-semibold " + (tone ?? "")}>{value}</div>
      {sub && <div className="mt-0.5 text-[11px] text-muted">{sub}</div>}
    </div>
  );
}

export function PageHeader({ title, sub, right }: { title: string; sub?: string; right?: ReactNode }) {
  return (
    <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
      <div>
        <h1 className="text-lg font-semibold">{title}</h1>
        {sub && <p className="mt-0.5 text-xs text-muted">{sub}</p>}
      </div>
      <div className="flex items-center gap-2">{right}</div>
    </div>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="px-4 py-10 text-center text-xs text-muted">{children}</div>;
}

export function ErrorBox({ error }: { error: Error | null }) {
  if (!error) return null;
  const status = (error as { status?: number }).status;
  const auth = status === 401 || status === 403;
  return (
    <div className="mb-3 rounded border border-block/50 bg-block/10 px-3 py-2 text-xs text-block">
      {auth ? "The gateway rejected the request. Open Settings and enter a valid API key." : error.message}
    </div>
  );
}

export function Drawer({ open, onClose, title, children, width = "max-w-2xl" }: { open: boolean; onClose: () => void; title: ReactNode; children: ReactNode; width?: string }) {
  useEffect(() => {
    if (!open) return;
    const h = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, [open, onClose]);
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-40 flex justify-end">
      <div className="absolute inset-0 bg-black/50" onClick={onClose} />
      <aside className={"scroll-thin relative z-10 h-full w-full overflow-y-auto border-l border-line bg-panel shadow-2xl " + width}>
        <header className="sticky top-0 z-10 flex items-center justify-between border-b border-line bg-panel px-4 py-3">
          <div className="min-w-0 truncate text-sm font-semibold">{title}</div>
          <button className="btn" onClick={onClose}>
            Close
          </button>
        </header>
        <div className="p-4">{children}</div>
      </aside>
    </div>
  );
}

export function KV({ k, children }: { k: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[110px_1fr] gap-2 py-1">
      <div className="label pt-0.5">{k}</div>
      <div className="min-w-0 break-words font-mono text-xs">{children}</div>
    </div>
  );
}

export function Code({ children, className }: { children: ReactNode; className?: string }) {
  return <pre className={"scroll-thin overflow-auto whitespace-pre-wrap break-words rounded border border-line bg-bg p-3 font-mono text-[11px] leading-relaxed " + (className ?? "")}>{children}</pre>;
}

export function Toast({ msg }: { msg: { text: string; kind: "ok" | "err" } | null }) {
  if (!msg) return null;
  return (
    <div className={"fixed bottom-4 right-4 z-50 rounded border bg-panel px-3 py-2 text-xs shadow-xl " + (msg.kind === "ok" ? "border-allow text-allow" : "border-block text-block")}>{msg.text}</div>
  );
}

export function Tabs<T extends string>({ value, onChange, items }: { value: T; onChange: (v: T) => void; items: { id: T; label: string; count?: number }[] }) {
  return (
    <div className="inline-flex rounded border border-line bg-raised p-0.5">
      {items.map((it) => (
        <button key={it.id} onClick={() => onChange(it.id)} className={"rounded px-2.5 py-1 text-xs " + (value === it.id ? "bg-panel font-medium text-fg" : "text-muted hover:text-fg")}>
          {it.label}
          {it.count !== undefined && <span className="ml-1.5 font-mono text-[10px] text-muted">{it.count}</span>}
        </button>
      ))}
    </div>
  );
}
