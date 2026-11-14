"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, api } from "./api";

export interface Poll<T> {
  data: T | null;
  error: ApiError | Error | null;
  loading: boolean;
  reload: () => void;
}

export function usePoll<T>(path: string | null, ms = 5000): Poll<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<ApiError | Error | null>(null);
  const [loading, setLoading] = useState(true);
  const alive = useRef(true);
  const seq = useRef(0);

  const load = useCallback(async () => {
    if (!path) return;
    const mine = ++seq.current;
    try {
      const d = await api<T>(path);
      if (alive.current && mine === seq.current) {
        setData(d);
        setError(null);
      }
    } catch (e) {
      if (alive.current && mine === seq.current) setError(e as Error);
    } finally {
      if (alive.current && mine === seq.current) setLoading(false);
    }
  }, [path]);

  useEffect(() => {
    alive.current = true;
    setLoading(true);
    load();
    if (!ms) {
      return () => {
        alive.current = false;
      };
    }
    const t = setInterval(() => {
      if (typeof document === "undefined" || document.visibilityState === "visible") load();
    }, ms);
    return () => {
      alive.current = false;
      clearInterval(t);
    };
  }, [load, ms]);

  return { data, error, loading, reload: load };
}

export function useNow(ms = 1000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), ms);
    return () => clearInterval(t);
  }, [ms]);
  return now;
}

export function useToast() {
  const [msg, setMsg] = useState<{ text: string; kind: "ok" | "err" } | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const show = useCallback((text: string, kind: "ok" | "err" = "ok") => {
    setMsg({ text, kind });
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setMsg(null), 4000);
  }, []);
  return { msg, show };
}
