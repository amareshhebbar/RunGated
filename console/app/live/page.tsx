"use client";

import { useMemo, useState } from "react";
import { EventDrawer } from "@/components/event-drawer";
import { Card, Chip, Empty, PageHeader, VerdictBadge } from "@/components/ui";
import { useEventFeed } from "@/lib/feed";
import { atLeast, clock, summarize } from "@/lib/format";
import { VERDICTS, type RungatedEvent } from "@/lib/types";

export default function Live() {
  const feed = useEventFeed(1000, 300);
  const [min, setMin] = useState("allow");
  const [agent, setAgent] = useState("");
  const [text, setText] = useState("");
  const [kind, setKind] = useState("");
  const [system, setSystem] = useState(false);
  const [sel, setSel] = useState<RungatedEvent | null>(null);

  const agents = useMemo(() => Array.from(new Set(feed.events.map((e) => e.agent_id).filter(Boolean))).sort(), [feed.events]);
  const kinds = useMemo(() => Array.from(new Set(feed.events.map((e) => e.action?.kind).filter(Boolean) as string[])).sort(), [feed.events]);

  const rows = feed.events.filter((e) => {
    if (e.type !== "decision") return system;
    if (!atLeast(e.decision?.verdict, min)) return false;
    if (agent && e.agent_id !== agent) return false;
    if (kind && e.action?.kind !== kind) return false;
    if (text) {
      const hay = (summarize(e.action) + " " + (e.decision?.reasons ?? []).join(" ") + " " + e.session_id).toLowerCase();
      if (!hay.includes(text.toLowerCase())) return false;
    }
    return true;
  });

  return (
    <div>
      <PageHeader
        title="Live actions"
        sub="Every decision as it happens. Click a row for findings, redactions and policy reasons."
        right={
          <>
            <span className="flex items-center gap-1.5 text-[11px] text-muted">
              <span className={"h-1.5 w-1.5 rounded-full " + (feed.state === "open" ? "bg-allow" : "bg-warn")} />
              {feed.state === "open" ? "streaming" : "reconnecting"}
            </span>
            <button className="btn" onClick={() => feed.setPause(!feed.paused)}>
              {feed.paused ? "Resume" : "Pause"}
            </button>
            <button className="btn" onClick={feed.clear}>
              Clear
            </button>
          </>
        }
      />
      <div className="mb-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-5">
        <select className="input" value={min} onChange={(e) => setMin(e.target.value)}>
          {VERDICTS.map((v) => (
            <option key={v} value={v}>
              {v === "allow" ? "All verdicts" : v + " and above"}
            </option>
          ))}
        </select>
        <select className="input" value={agent} onChange={(e) => setAgent(e.target.value)}>
          <option value="">All agents</option>
          {agents.map((a) => (
            <option key={a}>{a}</option>
          ))}
        </select>
        <select className="input" value={kind} onChange={(e) => setKind(e.target.value)}>
          <option value="">All kinds</option>
          {kinds.map((a) => (
            <option key={a}>{a}</option>
          ))}
        </select>
        <input className="input" placeholder="Search tool, reason, session" value={text} onChange={(e) => setText(e.target.value)} />
        <label className="flex items-center gap-2 text-xs text-muted">
          <input type="checkbox" checked={system} onChange={(e) => setSystem(e.target.checked)} />
          Include system events
        </label>
      </div>
      <Card title={rows.length + " events" + (feed.paused ? " (paused)" : "")}>
        {rows.length === 0 ? (
          <Empty>Nothing matches. Send traffic through the gateway or relax the filters.</Empty>
        ) : (
          <div className="scroll-thin max-h-[calc(100vh-280px)] overflow-auto">
            <table className="w-full text-xs">
              <thead className="sticky top-0 bg-panel">
                <tr className="border-b border-line">
                  <th className="th">Time</th>
                  <th className="th">Verdict</th>
                  <th className="th">Score</th>
                  <th className="th">Agent</th>
                  <th className="th">Channel</th>
                  <th className="th">Action</th>
                  <th className="th">Findings</th>
                  <th className="th">Why</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {rows.map((e) => (
                  <tr key={e.id} className="cursor-pointer hover:bg-raised" onClick={() => setSel(e)}>
                    <td className="td font-mono text-muted">{clock(e.time)}</td>
                    <td className="td">{e.decision ? <VerdictBadge verdict={e.decision.verdict} small /> : <Chip>{e.type}</Chip>}</td>
                    <td className="td font-mono">{e.decision?.score ?? ""}</td>
                    <td className="td font-mono">{e.agent_id}</td>
                    <td className="td font-mono text-muted">{e.action?.channel ?? ""}</td>
                    <td className="td font-mono">{e.action ? summarize(e.action) : e.message}</td>
                    <td className="td">
                      <div className="flex flex-wrap gap-1">
                        {Array.from(new Set((e.decision?.findings ?? []).map((f) => f.category))).map((c) => (
                          <Chip key={c}>{c}</Chip>
                        ))}
                      </div>
                    </td>
                    <td className="td max-w-[360px] truncate text-muted">{e.decision?.reasons?.[0] ?? ""}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      <EventDrawer event={sel} onClose={() => setSel(null)} />
    </div>
  );
}
