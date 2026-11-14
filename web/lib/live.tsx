"use client";

// One WebSocket per tab to /api/live. Pages subscribe with useLive(); the
// socket reconnects with backoff and tells subscribers when it is back, so
// they can reload anything they may have missed.

import { createContext, ReactNode, useContext, useEffect, useRef, useState } from "react";

export type LiveEvent = { type: string; data: any };
type Handler = (e: LiveEvent) => void;

type LiveApi = {
  subscribe: (h: Handler) => () => void;
  send: (msg: Record<string, unknown>) => void;
  connected: boolean;
};

const LiveContext = createContext<LiveApi | null>(null);

export function LiveProvider({ children }: { children: ReactNode }) {
  const handlers = useRef(new Set<Handler>());
  const sock = useRef<WebSocket | null>(null);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    let stopped = false;
    let retry = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let wasOpen = false;

    function connect() {
      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      const ws = new WebSocket(`${proto}//${location.host}/api/live`);
      sock.current = ws;
      ws.onopen = () => {
        if (sock.current !== ws) return;
        retry = 0;
        setConnected(true);
        // After a drop, pages reload what they show.
        if (wasOpen) handlers.current.forEach((h) => h({ type: "reconnected", data: {} }));
        wasOpen = true;
      };
      ws.onmessage = (m) => {
        if (sock.current !== ws) return;
        let e: LiveEvent;
        try {
          e = JSON.parse(m.data);
        } catch {
          return;
        }
        handlers.current.forEach((h) => h(e));
      };
      ws.onclose = (e) => {
        // A socket closed during a remount must not drop the newer one.
        if (sock.current !== ws) return;
        setConnected(false);
        sock.current = null;
        if (stopped) return;
        if (e.code === 1008) {
          // The session ended (signed out elsewhere or deactivated).
          window.location.href = "/login";
          return;
        }
        const wait = Math.min(30000, 1000 * 2 ** retry) + Math.random() * 500;
        retry++;
        timer = setTimeout(connect, wait);
      };
    }
    connect();
    return () => {
      stopped = true;
      clearTimeout(timer);
      const ws = sock.current;
      sock.current = null;
      ws?.close();
    };
  }, []);

  const api = useRef<LiveApi>({
    subscribe: (h) => {
      handlers.current.add(h);
      return () => handlers.current.delete(h);
    },
    send: (msg) => {
      if (sock.current?.readyState === WebSocket.OPEN) sock.current.send(JSON.stringify(msg));
    },
    connected: false,
  });
  api.current.connected = connected;

  return <LiveContext.Provider value={{ ...api.current, connected }}>{children}</LiveContext.Provider>;
}

/** Calls handler for every live event while the component is mounted. */
export function useLive(handler: Handler) {
  const live = useContext(LiveContext);
  const ref = useRef(handler);
  ref.current = handler;
  useEffect(() => {
    if (!live) return;
    return live.subscribe((e) => ref.current(e));
  }, [live?.subscribe]); // eslint-disable-line react-hooks/exhaustive-deps
}

export function useLiveSend() {
  const live = useContext(LiveContext);
  return live?.send ?? (() => {});
}

export function useLiveConnected() {
  return useContext(LiveContext)?.connected ?? false;
}
