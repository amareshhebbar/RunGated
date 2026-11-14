"use client";

import { useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";

type Label = "correct" | "false_positive" | "false_negative";

const OPTIONS: { label: Label; text: string; cls: string }[] = [
  { label: "correct", text: "Correct", cls: "btn btn-ok" },
  { label: "false_positive", text: "False alarm", cls: "btn" },
  { label: "false_negative", text: "Missed attack", cls: "btn btn-danger" },
];

const VERDICTS = ["allow", "warn", "redact", "hold", "block", "contain"];

export function FeedbackBar({ eventId }: { eventId: string }) {
  const [note, setNote] = useState("");
  const [expected, setExpected] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState<Label | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    setNote("");
    setExpected("");
    setSaved(null);
    setError("");
  }, [eventId]);

  const send = async (label: Label) => {
    setBusy(true);
    setError("");
    try {
      await api("/v1/events/" + encodeURIComponent(eventId) + "/feedback", { method: "POST", body: { label, note, expected } });
      setSaved(label);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "could not save feedback");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card space-y-2 px-3 py-3">
      <h3 className="label">Was this decision right?</h3>
      <div className="flex flex-wrap gap-2">
        {OPTIONS.map((o) => (
          <button key={o.label} className={o.cls} disabled={busy} onClick={() => send(o.label)} aria-pressed={saved === o.label}>
            {o.text}
          </button>
        ))}
      </div>
      <div className="flex flex-wrap gap-2">
        <select className="input" value={expected} onChange={(e) => setExpected(e.target.value)} aria-label="Expected verdict">
          <option value="">Expected verdict (optional)</option>
          {VERDICTS.map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
        </select>
        <input className="input flex-1" placeholder="Note (optional)" value={note} maxLength={2000} onChange={(e) => setNote(e.target.value)} />
      </div>
      {saved && <div className="text-xs text-allow">Saved as {saved.replace("_", " ")}. It is in the audit trail and the feedback export.</div>}
      {error && <div className="text-xs text-block">{error}</div>}
    </div>
  );
}
