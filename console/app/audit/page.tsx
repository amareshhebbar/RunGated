"use client";

import { useState } from "react";
import { Card, Chip, Empty, ErrorBox, PageHeader, Toast } from "@/components/ui";
import { ApiError, api } from "@/lib/api";
import { stamp } from "@/lib/format";
import { usePoll, useToast } from "@/lib/hooks";
import type { AuditRecord, VerifyResult } from "@/lib/types";

const PAGE = 100;

export default function Audit() {
  const [from, setFrom] = useState(1);
  const recs = usePoll<{ records: AuditRecord[] }>("/v1/audit?from=" + from + "&limit=" + PAGE, 0);
  const [result, setResult] = useState<VerifyResult | null>(null);
  const [running, setRunning] = useState(false);
  const toast = useToast();

  const verify = async () => {
    setRunning(true);
    try {
      setResult(await api<VerifyResult>("/v1/audit/verify"));
    } catch (e) {
      if (e instanceof ApiError && e.status === 409 && e.body && typeof e.body === "object") {
        setResult(e.body as VerifyResult);
      } else {
        toast.show(e instanceof ApiError ? e.message : "Request failed", "err");
      }
    } finally {
      setRunning(false);
    }
  };

  const rows = recs.data?.records ?? [];

  return (
    <div>
      <PageHeader
        title="Audit"
        sub="Hash chained, append only record of every decision and control action"
        right={
          <button className="btn btn-primary" disabled={running} onClick={verify}>
            {running ? "Verifying" : "Verify chain"}
          </button>
        }
      />
      <ErrorBox error={recs.error} />
      {result && (
        <div className={"mb-4 rounded border px-4 py-3 text-xs " + (result.ok ? "border-allow/50 bg-allow/10" : "border-block/50 bg-block/10")}>
          <div className={"text-sm font-semibold " + (result.ok ? "text-allow" : "text-block")}>{result.ok ? "Chain intact" : "Chain broken"}</div>
          <div className="mt-1 text-muted">
            {result.records} records and {result.events_checked} event bodies checked.
            {result.ok && result.head_hash ? " Head hash " + result.head_hash.slice(0, 24) + "." : ""}
          </div>
          {!result.ok && (
            <div className="mt-1 text-block">
              First bad record at sequence {result.first_bad_seq}. {result.error}
            </div>
          )}
        </div>
      )}
      <Card
        title={"Records from " + from}
        right={
          <>
            <button className="btn" disabled={from <= 1} onClick={() => setFrom(Math.max(1, from - PAGE))}>
              Previous
            </button>
            <button className="btn" disabled={rows.length < PAGE} onClick={() => setFrom(from + PAGE)}>
              Next
            </button>
          </>
        }
      >
        {rows.length === 0 ? (
          <Empty>No audit records in this range.</Empty>
        ) : (
          <div className="scroll-thin overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-line">
                  <th className="th">Seq</th>
                  <th className="th">Time</th>
                  <th className="th">Type</th>
                  <th className="th">Agent</th>
                  <th className="th">Tenant</th>
                  <th className="th">Event</th>
                  <th className="th">Hash</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {rows.map((r) => (
                  <tr key={r.seq}>
                    <td className="td font-mono">{r.seq}</td>
                    <td className="td text-muted">{stamp(r.time)}</td>
                    <td className="td"><Chip>{r.type}</Chip></td>
                    <td className="td font-mono">{r.agent_id}</td>
                    <td className="td font-mono">{r.tenant}</td>
                    <td className="td font-mono text-muted">{r.event_id}</td>
                    <td className="td font-mono text-muted">{r.hash ? r.hash.slice(0, 16) : ""}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      <Toast msg={toast.msg} />
    </div>
  );
}
