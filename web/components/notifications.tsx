"use client";

import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { api } from "@/lib/api";
import { ago, Notification } from "@/lib/chat";
import { useLive } from "@/lib/live";
import { cx } from "./ui";

function BellIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9 M13.7 21a2 2 0 0 1-3.4 0" />
    </svg>
  );
}

const kindLabel: Record<Notification["kind"], string> = {
  task_assigned: "Assigned",
  task_mention: "Mention",
  task_comment: "Comment",
  chat_mention: "Mention",
};

/** The bell: unread count, the latest 50 notifications and a short toast for new ones. */
export function NotificationBell({ align = "left" }: { align?: "left" | "right" }) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<Notification[] | null>(null);
  const [unread, setUnread] = useState(0);
  const [toast, setToast] = useState<Notification | null>(null);
  const panel = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);

  async function load() {
    try {
      const d = await api<{ items: Notification[]; unread: number }>("/notifications");
      setItems(d.items);
      setUnread(d.unread);
    } catch {
      /* the shell keeps working without notifications */
    }
  }

  useEffect(() => {
    load();
  }, []);

  useLive((e) => {
    if (e.type === "notification") {
      const n = e.data as Notification;
      setItems((cur) => (cur ? [n, ...cur.filter((x) => x.id !== n.id)].slice(0, 50) : cur));
      setUnread((u) => u + 1);
      setToast(n);
    } else if (e.type === "notifications.read") {
      setUnread(e.data.unread);
      load();
    } else if (e.type === "reconnected") {
      load();
    }
  });

  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), 6000);
    return () => clearTimeout(t);
  }, [toast]);

  useEffect(() => {
    if (!open) return;
    function onDown(e: MouseEvent) {
      if (!panel.current?.contains(e.target as Node) && !button.current?.contains(e.target as Node)) setOpen(false);
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        setOpen(false);
        button.current?.focus();
      }
    }
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  async function markRead(body: { ids?: string[]; all?: boolean }) {
    try {
      const d = await api<{ unread: number }>("/notifications/read", { body });
      setUnread(d.unread);
      setItems((cur) => cur?.map((n) => (body.all || body.ids?.includes(n.id) ? { ...n, read: true } : n)) ?? cur);
    } catch {
      /* ignore */
    }
  }

  function go(n: Notification) {
    if (!n.read) markRead({ ids: [n.id] });
    setOpen(false);
    setToast(null);
    if (n.link) router.push(n.link);
  }

  return (
    <div className="relative">
      <button
        ref={button}
        type="button"
        aria-label={unread ? `Notifications, ${unread} unread` : "Notifications"}
        aria-expanded={open}
        onClick={() => {
          setOpen((o) => !o);
          if (!open) load();
        }}
        className="relative grid h-10 w-10 place-items-center rounded-[10px] text-ink-2 hover:bg-canvas"
      >
        <BellIcon />
        {unread > 0 && (
          <span className="absolute right-1 top-1 grid min-w-[18px] place-items-center rounded-full bg-bad px-1 text-[10px] font-bold leading-[18px] text-white">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>
      {open && (
        <div
          ref={panel}
          role="dialog"
          aria-label="Notifications"
          className={cx(
            "fixed inset-x-3 top-16 z-50 flex max-h-[70vh] flex-col overflow-hidden rounded-2xl border border-line bg-white shadow-xl sm:absolute sm:inset-x-auto sm:top-12 sm:w-[380px]",
            align === "left" ? "sm:left-0" : "sm:right-0",
          )}
        >
          <div className="flex items-center justify-between border-b border-line px-4 py-3">
            <h2 className="text-sm font-semibold">Notifications</h2>
            <button type="button" disabled={!unread} onClick={() => markRead({ all: true })} className="rounded-lg px-2 py-1 text-xs font-semibold text-brand hover:bg-brand-soft disabled:text-muted disabled:hover:bg-transparent">
              Mark all as read
            </button>
          </div>
          <ul className="flex-1 overflow-y-auto">
            {items === null && <li className="px-4 py-6 text-center text-sm text-muted">Loading…</li>}
            {items?.length === 0 && (
              <li className="px-4 py-8 text-center text-sm text-muted">Nothing yet. You will see task assignments, comments on your tasks and @mentions here.</li>
            )}
            {items?.map((n) => (
              <li key={n.id} className="border-b border-[#eef0f3] last:border-0">
                <button type="button" onClick={() => go(n)} className={cx("flex w-full gap-3 px-4 py-3 text-left hover:bg-canvas", !n.read && "bg-[#f6f8fe]")}>
                  <span aria-hidden="true" className={cx("mt-1.5 h-2 w-2 shrink-0 rounded-full", n.read ? "bg-transparent" : "bg-brand")} />
                  <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="text-sm font-semibold leading-snug">{n.title}</span>
                    {n.body && <span className="line-clamp-2 text-[13px] leading-snug text-ink-2">{n.body}</span>}
                    <span className="text-xs text-muted">
                      {kindLabel[n.kind] ?? "Update"} · {ago(n.created_at)}
                      {!n.read && <span className="sr-only"> · unread</span>}
                    </span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
      {toast && !open && createPortal(
        <div role="status" className="fixed bottom-4 right-4 z-50 w-[min(360px,calc(100vw-2rem))] rounded-2xl border border-line bg-white p-4 shadow-xl">
          <div className="flex items-start gap-3">
            <span className="mt-0.5 text-brand">
              <BellIcon />
            </span>
            <button type="button" onClick={() => go(toast)} className="flex min-w-0 flex-1 flex-col gap-0.5 text-left">
              <span className="text-sm font-semibold leading-snug">{toast.title}</span>
              {toast.body && <span className="line-clamp-2 text-[13px] text-ink-2">{toast.body}</span>}
            </button>
            <button type="button" aria-label="Dismiss" onClick={() => setToast(null)} className="grid h-7 w-7 place-items-center rounded-lg text-muted hover:bg-canvas">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          </div>
        </div>,
        document.body,
      )}
    </div>
  );
}
