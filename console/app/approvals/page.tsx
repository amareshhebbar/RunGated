"use client";

import { useState } from "react";
import { Card, Chip, Code, Empty, ErrorBox, PageHeader, Tabs, Toast, VerdictBadge } from "@/components/ui";
import { ApiError, api } from "@/lib/api";
import { ago, countdown, summarize } from "@/lib/format";
import { useNow, usePoll, useToast } from "@/lib/hooks";
import type { Approval } from "@/lib/types";

export default function Approvals() {
  const [tab, setTab] = useState<"pending" | "history">("pending");
  const pending = usePoll<{ approvals: Approval[] }>("/v1/approvals?status=pending&limit=200", 2000);
  const all = usePoll<{ approvals: Approval[] }>(tab === "history" ? "/v1/approvals?limit=200" : null, 5000);
  const now = useNow(1000);
  const toast = useToast();
  const [reasons, setReasons] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");
  const [open, setOpen] = useState<Record<string, boolean>>({});

  const decide = async (id: string, approve: boolean) => {
    setBusy(id);
    try {
      await api("/v1/approvals/" + encodeURIComponent(id) + (approve ? "/approve" : "/deny"), { method: "POST", body: { reason: reasons[id] ?? "" } });
      toast.show(approve ? "Approved" : "Denied");
      pending.reload();
      all.reload();
    } catch (e) {
      toast.show(e instanceof ApiError ? e.message : "Request failed", "err");
      pending.reload();
    } finally {
      setBusy("");
    }
  };

  const rows = (tab === "pending" ? pending.data?.approvals : all.data?.approvals?.filter((a) => a.status !== "pending")) ?? [];
  const count = pending.data?.approvals?.length ?? 0;

  return (
    <div>
      <PageHeader
        title="Approvals"
        sub="Actions and MCP tool definitions waiting for a human decision"
        right={
          <Tabs
            value={tab}
            onChange={setTab}
            items={[
              { id: "pending", label: "Pending", count },
              { id: "history", label: "History" },
            ]}
          />
        }
      />
      <ErrorBox error={pending.error} />
      {rows.length === 0 ? (
        <Card>
          <Empty>{tab === "pending" ? "Nothing is waiting for approval." : "No resolved approvals yet."}</Empty>
        </Card>
      ) : (
        <div className="space-y-3">
          {rows.map((a) => {
            const left = countdown(a.expires_at, now);
            const isPending = a.status === "pending";
            return (
              <Card key={a.id}>
                <div className="flex flex-wrap items-start justify-between gap-3 p-3">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      {a.decision && <VerdictBadge verdict={a.decision.verdict} small />}
                      <Chip tone={a.kind === "pin" ? "accent" : "muted"}>{a.kind === "pin" ? "MCP tool pin" : "action"}</Chip>
                      <span className="font-mono text-xs">{a.agent_id}</span>
                      {a.action && <span className="font-mono text-xs text-muted">{summarize(a.action)}</span>}
                    </div>
                    <div className="mt-1.5 text-sm">{a.summary}</div>
                    <div className="mt-1 text-[11px] text-muted">
                      requested {ago(a.created_at, now)}
                      {isPending ? ", times out in " + left + " (on timeout: " + a.on_timeout + ")" : ", " + a.status + (a.decided_by ? " by " + a.decided_by : "") + (a.reason ? ": " + a.reason : "")}
                    </div>
                  </div>
                  {isPending && (
                    <div className="flex w-full flex-col gap-2 sm:w-72">
                      <input className="input" placeholder="Reason (optional)" value={reasons[a.id] ?? ""} onChange={(e) => setReasons({ ...reasons, [a.id]: e.target.value })} />
                      <div className="flex gap-2">
                        <button className="btn btn-ok flex-1" disabled={busy === a.id} onClick={() => decide(a.id, true)}>
                          Approve
                        </button>
                        <button className="btn btn-danger flex-1" disabled={busy === a.id} onClick={() => decide(a.id, false)}>
                          Deny
                        </button>
                      </div>
                    </div>
                  )}
                </div>
                <div className="border-t border-line px-3 py-1.5">
                  <button className="text-[11px] text-accent hover:underline" onClick={() => setOpen({ ...open, [a.id]: !open[a.id] })}>
                    {open[a.id] ? "Hide details" : "Show details"}
                  </button>
                  {open[a.id] && (
                    <div className="space-y-3 py-2">
                      {a.decision?.reasons && a.decision.reasons.length > 0 && (
                        <ul className="list-disc space-y-1 pl-5 text-xs">
                          {a.decision.reasons.map((r, i) => (
                            <li key={i}>{r}</li>
                          ))}
                        </ul>
                      )}
                      {a.decision?.findings && a.decision.findings.length > 0 && (
                        <div className="space-y-1">
                          {a.decision.findings.map((f, i) => (
                            <div key={i} className="flex items-start gap-2 text-xs">
                              <Chip>{f.category}</Chip>
                              <span>{f.message}</span>
                            </div>
                          ))}
                        </div>
                      )}
                      {a.action?.args && <Code>{JSON.stringify(a.action.args, null, 2)}</Code>}
                    </div>
                  )}
                </div>
              </Card>
            );
          })}
        </div>
      )}
      <Toast msg={toast.msg} />
    </div>
  );
}
