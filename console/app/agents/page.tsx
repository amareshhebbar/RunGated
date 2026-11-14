"use client";

import { useState } from "react";
import { Card, Chip, Empty, ErrorBox, PageHeader, Toast } from "@/components/ui";
import { ApiError, api } from "@/lib/api";
import { ago } from "@/lib/format";
import { useNow, usePoll, useToast } from "@/lib/hooks";
import { MODES, type Agent, type Containment } from "@/lib/types";

const riskTone: Record<string, "muted" | "warn" | "block" | "allow"> = { low: "allow", medium: "muted", high: "warn", critical: "block" };

export default function Agents() {
  const agents = usePoll<{ agents: Agent[] }>("/v1/agents", 4000);
  const cont = usePoll<{ containments: Containment[] }>("/v1/containments", 4000);
  const now = useNow(5000);
  const toast = useToast();
  const [busy, setBusy] = useState("");
  const [reason, setReason] = useState<Record<string, string>>({});

  const containedBy = new Map<string, Containment>();
  (cont.data?.containments ?? []).forEach((c) => {
    if (!c.released_at) containedBy.set(c.agent_id, c);
  });

  const act = async (id: string, fn: () => Promise<unknown>, ok: string) => {
    setBusy(id);
    try {
      await fn();
      toast.show(ok);
      agents.reload();
      cont.reload();
    } catch (e) {
      toast.show(e instanceof ApiError ? e.message : "Request failed", "err");
    } finally {
      setBusy("");
    }
  };

  const list = agents.data?.agents ?? [];

  return (
    <div>
      <PageHeader title="Agents" sub="Registered agent manifests, their autonomy mode and containment state" />
      <ErrorBox error={agents.error ?? cont.error} />
      <Card>
        {list.length === 0 ? (
          <Empty>No agents registered. Register a manifest with the CLI or POST /v1/agents.</Empty>
        ) : (
          <div className="scroll-thin overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-line">
                  <th className="th">Agent</th>
                  <th className="th">Tenant</th>
                  <th className="th">Risk tier</th>
                  <th className="th">Autonomy mode</th>
                  <th className="th">Tools</th>
                  <th className="th">Status</th>
                  <th className="th">Last seen</th>
                  <th className="th">Containment</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {list.map((a) => {
                  const c = containedBy.get(a.id);
                  return (
                    <tr key={a.id}>
                      <td className="td">
                        <div className="font-mono text-sm">{a.id}</div>
                        <div className="text-muted">{a.name || a.purpose}</div>
                        <div className="text-[11px] text-muted">{a.owner}</div>
                      </td>
                      <td className="td font-mono">{a.tenant}</td>
                      <td className="td"><Chip tone={riskTone[a.risk_tier] ?? "muted"}>{a.risk_tier}</Chip></td>
                      <td className="td">
                        <select
                          className="input w-40"
                          value={a.mode_override || a.mode}
                          disabled={busy === a.id}
                          onChange={(e) => act(a.id, () => api("/v1/agents/" + encodeURIComponent(a.id) + "/mode", { method: "PUT", body: { mode: e.target.value } }), "Mode set to " + e.target.value)}
                        >
                          {MODES.map((m) => (
                            <option key={m} value={m}>
                              {m}
                            </option>
                          ))}
                        </select>
                        {a.mode_override && <div className="mt-1 text-[11px] text-warn">override, manifest says {a.mode}</div>}
                      </td>
                      <td className="td font-mono">{a.tools} tools, {a.mcp_servers} mcp</td>
                      <td className="td">{c ? <Chip tone="block">contained</Chip> : <Chip tone="allow">{a.status || "active"}</Chip>}</td>
                      <td className="td text-muted">{ago(a.last_seen, now)}</td>
                      <td className="td">
                        {c ? (
                          <div className="space-y-1">
                            <div className="max-w-[260px] text-[11px] text-muted">{c.reason}</div>
                            <button className="btn btn-ok" disabled={busy === a.id} onClick={() => act(a.id, () => api("/v1/release", { method: "POST", body: { id: c.id } }), "Released " + a.id)}>
                              Release
                            </button>
                          </div>
                        ) : (
                          <div className="flex items-center gap-1.5">
                            <input className="input w-36" placeholder="Reason" value={reason[a.id] ?? ""} onChange={(e) => setReason({ ...reason, [a.id]: e.target.value })} />
                            <button className="btn btn-danger" disabled={busy === a.id} onClick={() => act(a.id, () => api("/v1/contain", { method: "POST", body: { agent_id: a.id, reason: reason[a.id] || "manual containment from console" } }), "Contained " + a.id)}>
                              Contain
                            </button>
                          </div>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      <Toast msg={toast.msg} />
    </div>
  );
}
