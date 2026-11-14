"use client";

import { useEffect, useState } from "react";
import { Card, Code, Empty, ErrorBox, PageHeader, Tabs, Toast } from "@/components/ui";
import { ApiError, api } from "@/lib/api";
import { stamp } from "@/lib/format";
import { usePoll, useToast } from "@/lib/hooks";
import type { Agent, AgentDetail, PolicyRecord } from "@/lib/types";

interface TenantPolicy {
  tenant: string;
  source: string;
  version: number;
  author?: string;
  updated_at?: string;
}

function TenantEditor({ tenant }: { tenant: string }) {
  const cur = usePoll<TenantPolicy>("/v1/policies/" + encodeURIComponent(tenant), 0);
  const hist = usePoll<{ history: PolicyRecord[] }>("/v1/policies/" + encodeURIComponent(tenant) + "/history", 0);
  const [src, setSrc] = useState("");
  const [dirty, setDirty] = useState(false);
  const [check, setCheck] = useState<{ valid: boolean; error?: string } | null>(null);
  const toast = useToast();

  useEffect(() => {
    if (cur.data && !dirty) setSrc(cur.data.source ?? "");
  }, [cur.data, dirty]);

  const validate = async () => {
    try {
      setCheck(await api<{ valid: boolean; error?: string }>("/v1/policies/validate", { method: "POST", body: { source: src } }));
    } catch (e) {
      toast.show(e instanceof ApiError ? e.message : "Request failed", "err");
    }
  };

  const save = async () => {
    try {
      await api("/v1/policies/" + encodeURIComponent(tenant), { method: "PUT", body: { source: src } });
      toast.show("Policy saved");
      setDirty(false);
      setCheck(null);
      cur.reload();
      hist.reload();
    } catch (e) {
      toast.show(e instanceof ApiError ? e.message : "Request failed", "err");
    }
  };

  return (
    <div className="grid gap-4 lg:grid-cols-3">
      <Card
        className="lg:col-span-2"
        title={"Custom Cedar for tenant " + tenant + (cur.data ? " (version " + cur.data.version + ")" : "")}
        right={
          <>
            <button className="btn" onClick={validate}>
              Validate
            </button>
            <button className="btn btn-primary" disabled={!dirty} onClick={save}>
              Save
            </button>
          </>
        }
      >
        <div className="p-3">
          <p className="mb-2 text-xs text-muted">
            Tenant policies are added on top of the policies compiled from each agent manifest. A forbid here always wins. Example: forbid (principal, action == Action::"tool.call", resource) when {"{"} context.tool == "delete_account" {"}"};
          </p>
          <textarea
            className="input h-80 resize-y font-mono text-[12px] leading-relaxed"
            spellCheck={false}
            value={src}
            onChange={(e) => {
              setSrc(e.target.value);
              setDirty(true);
              setCheck(null);
            }}
          />
          {check && (
            <div className={"mt-2 rounded border px-3 py-2 text-xs " + (check.valid ? "border-allow/50 text-allow" : "border-block/50 text-block")}>{check.valid ? "Valid Cedar policy" : check.error}</div>
          )}
        </div>
      </Card>
      <Card title="Version history">
        {(hist.data?.history ?? []).length === 0 ? (
          <Empty>No saved versions.</Empty>
        ) : (
          <ul className="divide-y divide-line">
            {[...(hist.data?.history ?? [])]
              .sort((a, b) => b.version - a.version)
              .map((h) => (
                <li key={h.version} className="flex items-center justify-between gap-2 px-3 py-2 text-xs">
                  <div>
                    <div className="font-mono">version {h.version}</div>
                    <div className="text-muted">{h.author} on {stamp(h.updated_at)}</div>
                  </div>
                  <button
                    className="btn"
                    onClick={() => {
                      setSrc(h.source);
                      setDirty(true);
                      setCheck(null);
                    }}
                  >
                    Load
                  </button>
                </li>
              ))}
          </ul>
        )}
      </Card>
      <Toast msg={toast.msg} />
    </div>
  );
}

export default function Policy() {
  const agents = usePoll<{ agents: Agent[] }>("/v1/agents", 0);
  const [id, setId] = useState("");
  const [tab, setTab] = useState<"manifest" | "cedar" | "tenant">("manifest");
  const list = agents.data?.agents ?? [];

  useEffect(() => {
    if (!id && list.length) setId(list[0].id);
  }, [id, list]);

  const detail = usePoll<AgentDetail>(id ? "/v1/agents/" + encodeURIComponent(id) : null, 0);
  const tenant = list.find((a) => a.id === id)?.tenant ?? "";

  return (
    <div>
      <PageHeader
        title="Policy"
        sub="Agent manifests compile to Cedar. Tenant policies add organisation wide rules."
        right={
          <>
            <select className="input w-56" value={id} onChange={(e) => setId(e.target.value)}>
              {list.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.id} ({a.tenant})
                </option>
              ))}
            </select>
            <Tabs
              value={tab}
              onChange={setTab}
              items={[
                { id: "manifest", label: "Manifest" },
                { id: "cedar", label: "Compiled Cedar" },
                { id: "tenant", label: "Tenant policy" },
              ]}
            />
          </>
        }
      />
      <ErrorBox error={agents.error ?? detail.error} />
      {list.length === 0 ? (
        <Card>
          <Empty>No agents registered yet.</Empty>
        </Card>
      ) : tab === "tenant" ? (
        tenant ? <TenantEditor key={tenant} tenant={tenant} /> : null
      ) : (
        <div className="grid gap-4 lg:grid-cols-3">
          <Card title={tab === "manifest" ? "rungated.agent.yaml" : "Cedar compiled from the manifest"} className="lg:col-span-2">
            <div className="p-3">
              <Code className="max-h-[70vh]">{tab === "manifest" ? detail.data?.manifest ?? "" : detail.data?.cedar ?? ""}</Code>
            </div>
          </Card>
          <Card title="Live usage against limits">
            {detail.data && Object.keys(detail.data.usage ?? {}).length > 0 ? (
              <table className="w-full text-xs">
                <tbody className="divide-y divide-line">
                  {Object.entries(detail.data.usage).map(([k, v]) => (
                    <tr key={k}>
                      <td className="td font-mono text-muted">{k}</td>
                      <td className="td text-right font-mono">{Math.round(v * 100) / 100}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <Empty>No usage recorded yet.</Empty>
            )}
          </Card>
        </div>
      )}
    </div>
  );
}
