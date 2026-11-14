"use client";

import type { RungatedEvent } from "@/lib/types";
import { micros, stamp, summarize } from "@/lib/format";
import { Chip, Code, Drawer, KV, VerdictBadge } from "./ui";
import { FeedbackBar } from "./feedback-bar";

export function EventDrawer({ event, onClose }: { event: RungatedEvent | null; onClose: () => void }) {
  const a = event?.action;
  const d = event?.decision;
  return (
    <Drawer open={!!event} onClose={onClose} title={event ? <span className="flex items-center gap-2">{d ? <VerdictBadge verdict={d.verdict} /> : <Chip>{event.type}</Chip>}<span className="truncate font-mono text-xs">{a ? summarize(a) : event.message}</span></span> : ""}>
      {event && (
        <div className="space-y-5">
          <div>
            <KV k="Time">{stamp(event.time)}</KV>
            <KV k="Agent">{event.agent_id || "none"}</KV>
            <KV k="Session">{event.session_id || "none"}</KV>
            <KV k="Event">{event.id}</KV>
            {a && <KV k="Channel">{a.channel}</KV>}
            {a?.model && <KV k="Model">{a.provider ? a.provider + " / " : ""}{a.model}</KV>}
            {a?.server && <KV k="Server">{a.server}</KV>}
            {a?.tool && <KV k="Tool">{a.tool}</KV>}
            {a?.url && <KV k="URL">{a.url}</KV>}
            {!a && event.message && <KV k="Message">{event.message}</KV>}
          </div>

          {d && (
            <div>
              <h3 className="label mb-2">Decision</h3>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                <div className="card px-3 py-2">
                  <div className="label">Risk score</div>
                  <div className="font-mono text-xl">{d.score}</div>
                </div>
                <div className="card px-3 py-2">
                  <div className="label">Computed</div>
                  <div className="mt-1"><VerdictBadge verdict={d.computed} small /></div>
                </div>
                <div className="card px-3 py-2">
                  <div className="label">Mode</div>
                  <div className="font-mono text-xs">{d.mode}</div>
                </div>
                <div className="card px-3 py-2">
                  <div className="label">Latency</div>
                  <div className="font-mono text-xs">{micros(d.latency_us)}</div>
                </div>
              </div>
              {d.reasons && d.reasons.length > 0 && (
                <ul className="mt-3 list-disc space-y-1 pl-5 text-xs">
                  {d.reasons.map((r, i) => (
                    <li key={i}>{r}</li>
                  ))}
                </ul>
              )}
              {d.judge_reason && <div className="mt-2 text-xs text-muted">Judge: {d.judge_reason}</div>}
              {d.degraded && <div className="mt-2 text-xs text-warn">A detector was unavailable. Fail mode: {d.fail_mode || "open"}.</div>}
              {d.approval_id && (
                <div className="mt-2 text-xs">
                  Approval <span className="font-mono">{d.approval_id}</span> {d.approval_status && <Chip>{d.approval_status}</Chip>}
                </div>
              )}
              {d.incident_id && (
                <div className="mt-1 text-xs">
                  Incident <a className="font-mono text-accent underline" href={"/incidents/?id=" + d.incident_id}>{d.incident_id}</a>
                </div>
              )}
            </div>
          )}

          {d && <FeedbackBar eventId={event.id} />}

          {d && d.policy_reasons && d.policy_reasons.length > 0 && (
            <div>
              <h3 className="label mb-2">Policy {d.policy_allowed ? "allowed" : "denied"}</h3>
              <div className="flex flex-wrap gap-1">
                {d.policy_reasons.map((r) => (
                  <Chip key={r}>{r}</Chip>
                ))}
              </div>
            </div>
          )}

          {d && d.findings && d.findings.length > 0 && (
            <div>
              <h3 className="label mb-2">Findings ({d.findings.length})</h3>
              <div className="overflow-hidden rounded border border-line">
                <table className="w-full text-xs">
                  <thead className="bg-raised">
                    <tr>
                      <th className="th">Detector</th>
                      <th className="th">Category</th>
                      <th className="th">Sev</th>
                      <th className="th">Conf</th>
                      <th className="th">Message</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-line">
                    {d.findings.map((f, i) => (
                      <tr key={i}>
                        <td className="td font-mono">{f.detector}<div className="text-[10px] text-muted">tier {f.tier}{f.rule ? " " + f.rule : ""}</div></td>
                        <td className="td"><Chip>{f.category}</Chip>{f.data_class && <div className="mt-1 font-mono text-[10px] text-muted">{f.data_class}</div>}</td>
                        <td className="td font-mono">{f.severity.toFixed(2)}</td>
                        <td className="td font-mono">{f.confidence.toFixed(2)}</td>
                        <td className="td">{f.message}{f.min_verdict && <div className="mt-1"><VerdictBadge verdict={f.min_verdict} small /></div>}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {d && d.redactions && d.redactions.length > 0 && (
            <div>
              <h3 className="label mb-2">Redactions ({d.redactions.length})</h3>
              <div className="flex flex-wrap gap-1">
                {d.redactions.map((r, i) => (
                  <Chip key={i} tone="accent">{r.class}: {r.token}</Chip>
                ))}
              </div>
            </div>
          )}

          {a?.args && Object.keys(a.args).length > 0 && (
            <div>
              <h3 className="label mb-2">Arguments</h3>
              <Code>{JSON.stringify(a.args, null, 2)}</Code>
            </div>
          )}

          {a?.texts && a.texts.length > 0 && (
            <div>
              <h3 className="label mb-2">Content ({a.texts.length} parts, sensitive values masked)</h3>
              <div className="space-y-2">
                {a.texts.map((t, i) => (
                  <div key={i}>
                    <div className="mb-1 flex items-center gap-2"><Chip>{t.source}</Chip>{t.path && <span className="font-mono text-[10px] text-muted">{t.path}</span>}</div>
                    <Code className="max-h-56">{t.text}</Code>
                  </div>
                ))}
              </div>
            </div>
          )}

          {event.data && (
            <div>
              <h3 className="label mb-2">Data</h3>
              <Code>{JSON.stringify(event.data, null, 2)}</Code>
            </div>
          )}
        </div>
      )}
    </Drawer>
  );
}
