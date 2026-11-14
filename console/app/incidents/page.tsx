"use client";

import { useCallback, useEffect, useState } from "react";
import { EventDrawer } from "@/components/event-drawer";
import { Card, Chip, Code, Empty, ErrorBox, KV, PageHeader, Tabs, Toast, VerdictBadge } from "@/components/ui";
import { ApiError, api } from "@/lib/api";
import { ago, clock, stamp, summarize } from "@/lib/format";
import { useNow, usePoll, useToast } from "@/lib/hooks";
import type { RungatedEvent, Incident } from "@/lib/types";

function Detail({ id, onBack, onChanged }: { id: string; onBack: () => void; onChanged: () => void }) {
  const d = usePoll<{ incident: Incident; events: RungatedEvent[] }>("/v1/incidents/" + encodeURIComponent(id), 4000);
  const [note, setNote] = useState("");
  const [sel, setSel] = useState<RungatedEvent | null>(null);
  const toast = useToast();
  const inc = d.data?.incident;
  const events = [...(d.data?.events ?? [])].sort((a, b) => (a.time < b.time ? -1 : 1));

  const resolve = async () => {
    try {
      await api("/v1/incidents/" + encodeURIComponent(id) + "/resolve", { method: "POST", body: { note } });
      toast.show("Incident resolved");
      d.reload();
      onChanged();
    } catch (e) {
      toast.show(e instanceof ApiError ? e.message : "Request failed", "err");
    }
  };

  return (
    <div>
      <PageHeader
        title={inc?.title ?? "Incident"}
        sub={id}
        right={
          <button className="btn" onClick={onBack}>
            Back to list
          </button>
        }
      />
      <ErrorBox error={d.error} />
      {inc && (
        <div className="grid gap-4 lg:grid-cols-3">
          <Card title="Summary" className="lg:col-span-1">
            <div className="p-3">
              <KV k="Verdict"><VerdictBadge verdict={inc.verdict} small /></KV>
              <KV k="Severity">{inc.severity}</KV>
              <KV k="Status">{inc.status}</KV>
              <KV k="Agent">{inc.agent_id}</KV>
              <KV k="Session">{inc.session_id || "none"}</KV>
              <KV k="Opened">{stamp(inc.created_at)}</KV>
              <KV k="Updated">{stamp(inc.updated_at)}</KV>
              <KV k="Categories"><div className="flex flex-wrap gap-1">{inc.categories?.map((c) => <Chip key={c}>{c}</Chip>)}</div></KV>
              {inc.snapshot_ref && <KV k="Snapshot">{inc.snapshot_ref}</KV>}
            </div>
            {inc.notes && inc.notes.length > 0 && (
              <div className="border-t border-line p-3">
                <div className="label mb-2">Notes</div>
                <Code>{inc.notes.join("\n")}</Code>
              </div>
            )}
            {inc.status !== "resolved" && (
              <div className="space-y-2 border-t border-line p-3">
                <input className="input" placeholder="Resolution note" value={note} onChange={(e) => setNote(e.target.value)} />
                <button className="btn btn-ok" onClick={resolve}>
                  Mark resolved
                </button>
              </div>
            )}
          </Card>
          <Card title={"Trace timeline (" + events.length + " events)"} className="lg:col-span-2">
            {events.length === 0 ? (
              <Empty>No events recorded for this incident.</Empty>
            ) : (
              <ol className="relative p-4">
                {events.map((e, i) => (
                  <li key={e.id} className="relative cursor-pointer pb-4 pl-6 last:pb-0" onClick={() => setSel(e)}>
                    {i < events.length - 1 && <span className="absolute left-[5px] top-3 h-full w-px bg-line" />}
                    <span className="absolute left-0 top-1.5 h-[11px] w-[11px] rounded-full border border-line bg-raised" />
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-mono text-[11px] text-muted">{clock(e.time)}</span>
                      {e.decision ? <VerdictBadge verdict={e.decision.verdict} small /> : <Chip>{e.type}</Chip>}
                      <span className="font-mono text-xs">{e.action ? summarize(e.action) : e.message}</span>
                    </div>
                    {e.decision?.reasons?.[0] && <div className="mt-0.5 text-xs text-muted">{e.decision.reasons[0]}</div>}
                  </li>
                ))}
              </ol>
            )}
          </Card>
        </div>
      )}
      <EventDrawer event={sel} onClose={() => setSel(null)} />
      <Toast msg={toast.msg} />
    </div>
  );
}

export default function Incidents() {
  const [status, setStatus] = useState<"open" | "resolved" | "all">("open");
  const q = status === "all" ? "" : "?status=" + status;
  const list = usePoll<{ incidents: Incident[] }>("/v1/incidents" + q, 4000);
  const now = useNow(5000);
  const [sel, setSel] = useState("");

  useEffect(() => {
    try {
      setSel(new URLSearchParams(window.location.search).get("id") ?? "");
    } catch {}
  }, []);

  const open = useCallback((id: string) => {
    setSel(id);
    try {
      window.history.replaceState(null, "", id ? "/incidents/?id=" + encodeURIComponent(id) : "/incidents/");
    } catch {}
  }, []);

  if (sel) return <Detail id={sel} onBack={() => open("")} onChanged={list.reload} />;

  const rows = list.data?.incidents ?? [];
  return (
    <div>
      <PageHeader
        title="Incidents"
        sub="Related decisions grouped per agent session, with a full trace"
        right={
          <Tabs
            value={status}
            onChange={setStatus}
            items={[
              { id: "open", label: "Open" },
              { id: "resolved", label: "Resolved" },
              { id: "all", label: "All" },
            ]}
          />
        }
      />
      <ErrorBox error={list.error} />
      <Card>
        {rows.length === 0 ? (
          <Empty>No incidents in this view.</Empty>
        ) : (
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b border-line">
                <th className="th">Opened</th>
                <th className="th">Verdict</th>
                <th className="th">Severity</th>
                <th className="th">Title</th>
                <th className="th">Agent</th>
                <th className="th">Categories</th>
                <th className="th">Events</th>
                <th className="th">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {rows.map((i) => (
                <tr key={i.id} className="cursor-pointer hover:bg-raised" onClick={() => open(i.id)}>
                  <td className="td text-muted">{ago(i.created_at, now)}</td>
                  <td className="td"><VerdictBadge verdict={i.verdict} small /></td>
                  <td className="td font-mono">{i.severity}</td>
                  <td className="td max-w-[380px] truncate">{i.title}</td>
                  <td className="td font-mono">{i.agent_id}</td>
                  <td className="td"><div className="flex flex-wrap gap-1">{i.categories?.slice(0, 4).map((c) => <Chip key={c}>{c}</Chip>)}</div></td>
                  <td className="td font-mono">{i.event_ids?.length ?? 0}</td>
                  <td className="td"><Chip tone={i.status === "resolved" ? "allow" : "warn"}>{i.status}</Chip></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
    </div>
  );
}
