"use client";

import { useState } from "react";
import { Card, Chip, Code, Empty, ErrorBox, PageHeader, Tabs, Toast } from "@/components/ui";
import { ApiError, api } from "@/lib/api";
import { wordDiff } from "@/lib/diff";
import { ago, stamp } from "@/lib/format";
import { useNow, usePoll, useToast } from "@/lib/hooks";
import type { Pin } from "@/lib/types";

function Diff({ before, after }: { before: string; after: string }) {
  const parts = wordDiff(before, after);
  return (
    <div className="scroll-thin max-h-64 overflow-auto whitespace-pre-wrap break-words rounded border border-line bg-bg p-3 font-mono text-[11px] leading-relaxed">
      {parts.map((p, i) =>
        p.kind === "same" ? (
          <span key={i}>{p.text}</span>
        ) : p.kind === "add" ? (
          <span key={i} className="bg-allow/20 text-allow">{p.text}</span>
        ) : (
          <span key={i} className="bg-block/20 text-block line-through">{p.text}</span>
        ),
      )}
    </div>
  );
}

function pretty(v: unknown): string {
  if (v === undefined || v === null) return "";
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}

export default function Pins() {
  const [tab, setTab] = useState<"pending" | "all">("pending");
  const pins = usePoll<{ pins: Pin[] }>("/v1/pins", 3000);
  const now = useNow(5000);
  const toast = useToast();
  const [busy, setBusy] = useState("");
  const [open, setOpen] = useState<Record<string, boolean>>({});

  const key = (p: Pin) => p.tenant + "/" + p.server + "/" + p.tool;
  const list = pins.data?.pins ?? [];
  const pendingList = list.filter((p) => p.pending_fingerprint || p.status === "pending");
  const rows = tab === "pending" ? pendingList : list;

  const decide = async (p: Pin, approve: boolean) => {
    setBusy(key(p));
    try {
      await api("/v1/pins/" + [p.tenant, p.server, p.tool].map(encodeURIComponent).join("/") + (approve ? "/approve" : "/reject"), { method: "POST", body: {} });
      toast.show(approve ? "Pin approved" : "Pin rejected");
      pins.reload();
    } catch (e) {
      toast.show(e instanceof ApiError ? e.message : "Request failed", "err");
    } finally {
      setBusy("");
    }
  };

  return (
    <div>
      <PageHeader
        title="MCP pins"
        sub="Every MCP tool is fingerprinted. A changed definition is hidden from the agent until a human approves it."
        right={
          <Tabs
            value={tab}
            onChange={setTab}
            items={[
              { id: "pending", label: "Needs review", count: pendingList.length },
              { id: "all", label: "All pins", count: list.length },
            ]}
          />
        }
      />
      <ErrorBox error={pins.error} />
      {rows.length === 0 ? (
        <Card>
          <Empty>{tab === "pending" ? "No tool definitions are waiting for review." : "No MCP tools have been seen yet."}</Empty>
        </Card>
      ) : (
        <div className="space-y-3">
          {rows.map((p) => {
            const changed = !!p.pending_fingerprint;
            const isNew = changed && !p.fingerprint;
            return (
              <Card key={key(p)}>
                <div className="flex flex-wrap items-start justify-between gap-3 p-3">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-mono text-sm">{p.server}/{p.tool}</span>
                      <Chip tone={p.status === "approved" ? "allow" : p.status === "rejected" ? "block" : "warn"}>{p.status}</Chip>
                      {changed && <Chip tone="block">{isNew ? "new tool awaiting approval" : "definition changed"}</Chip>}
                      <span className="font-mono text-[11px] text-muted">tenant {p.tenant}</span>
                    </div>
                    <div className="mt-1 text-[11px] text-muted">
                      first seen {ago(p.first_seen, now)}
                      {p.approved_by ? ", approved by " + p.approved_by : ""}
                      {changed && p.pending_since ? ", change seen " + ago(p.pending_since, now) : ""}
                    </div>
                    <div className="mt-1 font-mono text-[11px] text-muted">fingerprint {p.fingerprint ? p.fingerprint.slice(0, 16) : "none"}{changed ? " to " + (p.pending_fingerprint ?? "").slice(0, 16) : ""}</div>
                  </div>
                  {changed && (
                    <div className="flex gap-2">
                      <button className="btn btn-ok" disabled={busy === key(p)} onClick={() => decide(p, true)}>
                        Approve new definition
                      </button>
                      <button className="btn btn-danger" disabled={busy === key(p)} onClick={() => decide(p, false)}>
                        Reject
                      </button>
                    </div>
                  )}
                </div>
                <div className="space-y-3 border-t border-line p-3">
                  {changed ? (
                    <>
                      <div className="label">Description diff (red removed, green added)</div>
                      <Diff before={p.description ?? ""} after={p.pending_description ?? ""} />
                      {pretty(p.schema) !== pretty(p.pending_schema) && (
                        <>
                          <div className="label">Input schema diff</div>
                          <Diff before={pretty(p.schema)} after={pretty(p.pending_schema)} />
                        </>
                      )}
                    </>
                  ) : (
                    <>
                      <div className="label">Approved description</div>
                      <div className="whitespace-pre-wrap text-xs">{p.description || "No description"}</div>
                      <button className="text-[11px] text-accent hover:underline" onClick={() => setOpen({ ...open, [key(p)]: !open[key(p)] })}>
                        {open[key(p)] ? "Hide schema" : "Show input schema"}
                      </button>
                      {open[key(p)] && <Code>{pretty(p.schema) || "No schema"}</Code>}
                      <div className="text-[11px] text-muted">pinned {stamp(p.first_seen)}</div>
                    </>
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
