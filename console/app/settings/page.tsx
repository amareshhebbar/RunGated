"use client";

import { useEffect, useState } from "react";
import { Card, KV, PageHeader, Toast } from "@/components/ui";
import { ApiError, api, getBase, getKey, setBase, setKey } from "@/lib/api";
import { useToast } from "@/lib/hooks";

export default function Settings() {
  const [key, setKeyState] = useState("");
  const [base, setBaseState] = useState("");
  const [show, setShow] = useState(false);
  const [info, setInfo] = useState<{ version?: string; go?: string; status?: string } | null>(null);
  const toast = useToast();

  useEffect(() => {
    setKeyState(getKey());
    setBaseState(getBase());
  }, []);

  const test = async () => {
    try {
      const v = await api<{ version: string; go: string }>("/v1/version");
      const h = await api<{ status: string }>("/v1/health");
      await api("/v1/stats");
      setInfo({ version: v.version, go: v.go, status: h.status });
      toast.show("Connected and authorised");
    } catch (e) {
      setInfo(null);
      toast.show(e instanceof ApiError && (e.status === 401 || e.status === 403) ? "Gateway reachable but the key was rejected" : e instanceof Error ? e.message : "Connection failed", "err");
    }
  };

  const save = () => {
    setKey(key);
    setBase(base);
    toast.show("Saved in this browser");
  };

  const clear = () => {
    setKey("");
    setKeyState("");
    setInfo(null);
    toast.show("API key removed from this browser");
  };

  return (
    <div className="max-w-2xl">
      <PageHeader title="Settings" sub="Connection details are stored only in this browser" />
      <Card title="Gateway connection">
        <div className="space-y-4 p-4">
          <div>
            <label className="label">API key</label>
            <div className="mt-1 flex gap-2">
              <input className="input font-mono" type={show ? "text" : "password"} placeholder="Bearer key from auth.api_keys or RUNGATED_API_KEY" value={key} onChange={(e) => setKeyState(e.target.value)} autoComplete="off" />
              <button className="btn" onClick={() => setShow(!show)}>
                {show ? "Hide" : "Show"}
              </button>
            </div>
            <p className="mt-1 text-[11px] text-muted">Leave empty when the gateway runs in development mode.</p>
          </div>
          <div>
            <label className="label">Gateway base URL</label>
            <input className="input mt-1 font-mono" placeholder="Empty means the same origin that served this page" value={base} onChange={(e) => setBaseState(e.target.value)} />
          </div>
          <div className="flex gap-2">
            <button className="btn btn-primary" onClick={save}>
              Save
            </button>
            <button className="btn" onClick={test}>
              Test connection
            </button>
            <button className="btn btn-danger" onClick={clear}>
              Remove key
            </button>
          </div>
        </div>
      </Card>
      {info && (
        <Card title="Gateway" className="mt-4">
          <div className="p-4">
            <KV k="Version">{info.version}</KV>
            <KV k="Runtime">{info.go}</KV>
            <KV k="Health">{info.status}</KV>
          </div>
        </Card>
      )}
      <Toast msg={toast.msg} />
    </div>
  );
}
