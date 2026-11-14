"use client";

import { useState } from "react";
import { EventDrawer } from "@/components/event-drawer";
import { Card, Chip, Empty, ErrorBox, PageHeader, Stat, VerdictBadge } from "@/components/ui";
import { useEventFeed } from "@/lib/feed";
import { clock, micros, num, summarize, verdictBg } from "@/lib/format";
import { usePoll } from "@/lib/hooks";
import { VERDICTS, type RungatedEvent, type Stats, type Verdict } from "@/lib/types";

function Bars({ data, order }: { data: Record<string, number>; order?: string[] }) {
  const keys = order ?? Object.keys(data).sort((a, b) => (data[b] ?? 0) - (data[a] ?? 0));
  const max = Math.max(1, ...keys.map((k) => data[k] ?? 0));
  if (keys.length === 0) return <Empty>No data yet</Empty>;
  return (
    <div className="space-y-2 p-3">
      {keys.map((k) => {
        const v = data[k] ?? 0;
        const color = verdictBg[k as Verdict] ?? "bg-accent";
        return (
          <div key={k} className="grid grid-cols-[96px_1fr_64px] items-center gap-2 text-xs">
            <span className="truncate font-mono text-muted">{k}</span>
            <div className="h-2 rounded bg-raised">
              <div className={"h-2 rounded " + color} style={{ width: (v / max) * 100 + "%", minWidth: v > 0 ? 3 : 0 }} />
            </div>
            <span className="text-right font-mono">{num(v)}</span>
          </div>
        );
      })}
    </div>
  );
}

export default function Overview() {
  const stats = usePoll<Stats>("/v1/stats", 3000);
  const feed = useEventFeed(60, 30);
  const [sel, setSel] = useState<RungatedEvent | null>(null);
  const d = stats.data?.decisions;
  const decisions = feed.events.filter((e) => e.type === "decision").slice(0, 10);
  const blocked = (d?.verdicts?.block ?? 0) + (d?.verdicts?.contain ?? 0);

  return (
    <div>
      <PageHeader title="Overview" sub="Decisions across LLM, MCP and egress traffic since the gateway started" />
      <ErrorBox error={stats.error} />
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-6">
        <Stat label="Decisions" value={num(d?.total)} sub={d ? "uptime " + Math.floor(d.uptime_s / 60) + " min" : ""} />
        <Stat label="Blocked or contained" value={num(blocked)} tone={blocked > 0 ? "text-block" : ""} />
        <Stat label="Pending approvals" value={num(stats.data?.pending_approvals)} tone={(stats.data?.pending_approvals ?? 0) > 0 ? "text-hold" : ""} />
        <Stat label="Contained agents" value={num(stats.data?.active_containment)} tone={(stats.data?.active_containment ?? 0) > 0 ? "text-contain" : ""} />
        <Stat label="Latency p50" value={micros(d?.p50_us ?? 0)} sub={"p95 " + micros(d?.p95_us ?? 0)} />
        <Stat label="Agents" value={num(stats.data?.agents)} sub={"vault tokens " + num(stats.data?.vault?.tokens)} />
      </div>

      <div className="mt-4 grid gap-4 lg:grid-cols-3">
        <Card title="Verdicts">
          <Bars data={d?.verdicts ?? {}} order={VERDICTS} />
        </Card>
        <Card title="Finding categories">
          <Bars data={d?.categories ?? {}} />
        </Card>
        <Card title="Action kinds">
          <Bars data={d?.kinds ?? {}} />
        </Card>
      </div>

      <Card
        className="mt-4"
        title="Live feed"
        right={
          <span className="flex items-center gap-1.5 text-[11px] text-muted">
            <span className={"h-1.5 w-1.5 rounded-full " + (feed.state === "open" ? "bg-allow" : "bg-warn")} />
            {feed.state === "open" ? "streaming" : "connecting"}
          </span>
        }
      >
        {decisions.length === 0 ? (
          <Empty>No decisions yet. Send traffic through the gateway and it appears here.</Empty>
        ) : (
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-line">
                <th className="th">Time</th>
                <th className="th">Verdict</th>
                <th className="th">Score</th>
                <th className="th">Agent</th>
                <th className="th">Action</th>
                <th className="th">Why</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {decisions.map((e) => (
                <tr key={e.id} className="cursor-pointer hover:bg-raised" onClick={() => setSel(e)}>
                  <td className="td font-mono text-muted">{clock(e.time)}</td>
                  <td className="td"><VerdictBadge verdict={e.decision?.verdict} small /></td>
                  <td className="td font-mono">{e.decision?.score}</td>
                  <td className="td font-mono">{e.agent_id}</td>
                  <td className="td font-mono">{summarize(e.action)}</td>
                  <td className="td max-w-[420px] truncate text-muted">{e.decision?.reasons?.[0] ?? ""}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
      {d && d.degraded > 0 && (
        <div className="mt-3">
          <Chip tone="warn">{d.degraded} decisions ran with a degraded detector</Chip>
        </div>
      )}
      <EventDrawer event={sel} onClose={() => setSel(null)} />
    </div>
  );
}
