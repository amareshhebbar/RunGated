"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { api, openStream } from "./api";
import type { RungatedEvent } from "./types";

export function useEventFeed(max = 500, backfill = 100) {
  const [events, setEvents] = useState<RungatedEvent[]>([]);
  const [state, setState] = useState<"connecting" | "open" | "closed">("connecting");
  const [paused, setPaused] = useState(false);
  const pausedRef = useRef(false);
  const queue = useRef<RungatedEvent[]>([]);

  const merge = useCallback(
    (incoming: RungatedEvent[]) => {
      setEvents((prev) => {
        const seen = new Set(prev.map((e) => e.id));
        const fresh = incoming.filter((e) => !seen.has(e.id));
        if (fresh.length === 0) return prev;
        const next = [...fresh, ...prev].sort((a, b) => (a.time < b.time ? 1 : a.time > b.time ? -1 : 0));
        return next.length > max ? next.slice(0, max) : next;
      });
    },
    [max],
  );

  useEffect(() => {
    let alive = true;
    api<{ events: RungatedEvent[] }>("/v1/events?limit=" + backfill)
      .then((r) => {
        if (alive) merge(r.events ?? []);
      })
      .catch(() => {});
    const h = openStream(
      "/v1/events/stream",
      (_name, data) => {
        try {
          const ev = JSON.parse(data) as RungatedEvent;
          if (!ev || !ev.id) return;
          if (pausedRef.current) {
            queue.current.push(ev);
            if (queue.current.length > 2000) queue.current.shift();
          } else {
            merge([ev]);
          }
        } catch {}
      },
      (s) => setState(s),
    );
    return () => {
      alive = false;
      h.close();
    };
  }, [merge, backfill]);

  const setPause = useCallback(
    (p: boolean) => {
      pausedRef.current = p;
      setPaused(p);
      if (!p && queue.current.length) {
        const q = queue.current;
        queue.current = [];
        merge(q);
      }
    },
    [merge],
  );

  const clear = useCallback(() => setEvents([]), []);

  return { events, state, paused, setPause, clear };
}
