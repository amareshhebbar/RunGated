"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { ReactNode, useEffect, useState } from "react";
import { getTheme, setTheme } from "@/lib/api";
import { usePoll } from "@/lib/hooks";
import type { Stats } from "@/lib/types";

const NAV = [
  { href: "/", label: "Overview" },
  { href: "/live/", label: "Live actions" },
  { href: "/agents/", label: "Agents" },
  { href: "/incidents/", label: "Incidents" },
  { href: "/approvals/", label: "Approvals", badge: "approvals" },
  { href: "/pins/", label: "MCP pins" },
  { href: "/policy/", label: "Policy" },
  { href: "/audit/", label: "Audit" },
  { href: "/settings/", label: "Settings" },
];

export function Shell({ children }: { children: ReactNode }) {
  const path = usePathname() ?? "/";
  const [theme, setThemeState] = useState<"dark" | "light">("dark");
  const [open, setOpen] = useState(false);
  const stats = usePoll<Stats>("/v1/stats", 5000);

  useEffect(() => {
    const t = getTheme();
    setThemeState(t);
    document.documentElement.setAttribute("data-theme", t);
  }, []);

  useEffect(() => setOpen(false), [path]);

  const toggle = () => {
    const next = theme === "dark" ? "light" : "dark";
    setThemeState(next);
    setTheme(next);
    document.documentElement.setAttribute("data-theme", next);
  };

  const active = (href: string) => (href === "/" ? path === "/" : path.startsWith(href.replace(/\/$/, "")));
  const pending = stats.data?.pending_approvals ?? 0;
  const contained = stats.data?.active_containment ?? 0;
  const connected = !stats.error && !!stats.data;

  return (
    <div className="flex min-h-screen">
      <aside className={"fixed inset-y-0 left-0 z-30 w-56 shrink-0 border-r border-line bg-panel transition-transform md:static md:translate-x-0 " + (open ? "translate-x-0" : "-translate-x-full")}>
        <div className="flex h-12 items-center gap-2 border-b border-line px-4">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="rgb(var(--accent))" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6l8-3z" />
            <path d="M9 12l2 2 4-4" />
          </svg>
          <span className="text-sm font-semibold tracking-wide">Rungated</span>
        </div>
        <nav className="p-2">
          {NAV.map((n) => (
            <Link key={n.href} href={n.href} className={"mb-0.5 flex items-center justify-between rounded px-3 py-1.5 text-xs " + (active(n.href) ? "bg-raised font-medium text-fg" : "text-muted hover:bg-raised hover:text-fg")}>
              <span>{n.label}</span>
              {n.badge === "approvals" && pending > 0 && <span className="rounded bg-hold px-1.5 font-mono text-[10px] text-bg">{pending}</span>}
            </Link>
          ))}
        </nav>
        <div className="absolute inset-x-0 bottom-0 border-t border-line p-3 text-[11px] text-muted">
          <div className="flex items-center gap-1.5">
            <span className={"h-1.5 w-1.5 rounded-full " + (connected ? "bg-allow" : "bg-block")} />
            {connected ? "Gateway connected" : "Gateway unreachable"}
          </div>
          {contained > 0 && <div className="mt-1 text-contain">{contained} agent{contained === 1 ? "" : "s"} contained</div>}
        </div>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-20 flex h-12 items-center justify-between border-b border-line bg-panel/90 px-4 backdrop-blur">
          <button className="btn md:hidden" onClick={() => setOpen(!open)}>
            Menu
          </button>
          <div className="hidden text-xs text-muted md:block">Runtime immune system for AI agents</div>
          <div className="flex items-center gap-2">
            <button className="btn" onClick={toggle}>
              {theme === "dark" ? "Light mode" : "Dark mode"}
            </button>
          </div>
        </header>
        <main className="min-w-0 flex-1 p-4 md:p-6">{children}</main>
      </div>
    </div>
  );
}
