"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, ReactNode, useContext, useEffect, useState } from "react";
import { api, Me } from "@/lib/api";
import { LiveProvider, useLive } from "@/lib/live";
import { NotificationBell } from "./notifications";
import { cx } from "./ui";

const MeContext = createContext<Me | null>(null);

export function useMe(): Me {
  const me = useContext(MeContext);
  if (!me) throw new Error("useMe outside the app shell");
  return me;
}

type NavItem = { href: string; label: string; show: (me: Me) => boolean };

const sections: { title: string; items: NavItem[] }[] = [
  {
    title: "My work",
    items: [
      { href: "/dashboard", label: "Dashboard", show: () => true },
      { href: "/chat", label: "Chat", show: () => true },
      { href: "/attendance", label: "Attendance", show: () => true },
      { href: "/requests", label: "WFH and requests", show: () => true },
      { href: "/tasks", label: "Tasks", show: () => true },
      { href: "/timesheets", label: "Timesheets", show: () => true },
    ],
  },
  {
    title: "My team",
    items: [
      { href: "/team/attendance", label: "Team attendance", show: (m) => m.can.team_views },
      { href: "/team/timesheets", label: "Timesheet approvals", show: (m) => m.can.review_timesheets },
      { href: "/team/people", label: "People", show: (m) => m.can.team_views },
      { href: "/approvals", label: "Request approvals", show: (m) => m.can.team_views },
    ],
  },
  {
    title: "Admin",
    items: [
      { href: "/admin/people", label: "Teams and people", show: (m) => m.can.admin },
      { href: "/admin/rules", label: "Attendance rules", show: (m) => m.can.admin },
      { href: "/admin/calendar", label: "Holidays and projects", show: (m) => m.can.holidays },
    ],
  },
];

const roleLabel = { employee: "Employee", manager: "Manager", hr: "HR", admin: "Admin" };

export function AppShell({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null>(null);

  useEffect(() => {
    api<Me>("/me").then(setMe).catch(() => {});
  }, []);

  if (!me) {
    return <div className="grid min-h-screen place-items-center text-sm text-muted">Loading…</div>;
  }
  return (
    <LiveProvider>
      <Shell me={me}>{children}</Shell>
    </LiveProvider>
  );
}

/** Unread chat messages, kept fresh from live events. */
function useChatUnread(me: Me): number {
  const [n, setN] = useState(0);
  const timer = useState<{ t?: ReturnType<typeof setTimeout> }>({})[0];
  function load() {
    clearTimeout(timer.t);
    timer.t = setTimeout(() => {
      api<{ unread: number }>("/chat/unread").then((d) => setN(d.unread)).catch(() => {});
    }, 300);
  }
  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps
  useLive((e) => {
    if (e.type === "message.new" && e.data.message?.author?.id === me.id) return;
    if (["message.new", "message.delete", "read", "channel.changed", "reconnected"].includes(e.type)) load();
  });
  return n;
}

/** True at the lg breakpoint and up, where the sidebar is always shown. */
function useWide(): boolean {
  const [wide, setWide] = useState(false);
  useEffect(() => {
    const q = window.matchMedia("(min-width: 1024px)");
    const on = () => setWide(q.matches);
    on();
    q.addEventListener("change", on);
    return () => q.removeEventListener("change", on);
  }, []);
  return wide;
}

function Shell({ me, children }: { me: Me; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  // One bell only, so a new notification shows one toast.
  const wide = useWide();
  const path = usePathname();
  const router = useRouter();
  const chatUnread = useChatUnread(me);
  useEffect(() => setOpen(false), [path]);

  async function signOut() {
    await api("/auth/logout", { body: {} }).catch(() => {});
    router.replace("/login");
  }

  const nav = (
    <nav aria-label="Main" className="flex flex-1 flex-col gap-5 overflow-y-auto px-3 py-4">
      {sections.map((s) => {
        const items = s.items.filter((i) => i.show(me));
        if (!items.length) return null;
        return (
          <div key={s.title} className="flex flex-col gap-0.5">
            <span className="px-3 pb-1 text-[11px] font-semibold uppercase tracking-wider text-[#8a909c]">
              {s.title === "My team" && me.role !== "manager" ? "Company" : s.title}
            </span>
            {items.map((i) => {
              const active = path === i.href || path.startsWith(i.href + "/");
              return (
                <Link
                  key={i.href}
                  href={i.href}
                  aria-current={active ? "page" : undefined}
                  className={cx(
                    "flex min-h-10 items-center rounded-[10px] px-3 text-sm font-medium",
                    active ? "bg-brand-soft text-brand-ink" : "text-ink-2 hover:bg-canvas",
                  )}
                >
                  <span className="flex-1">{i.label}</span>
                  {i.href === "/chat" && chatUnread > 0 && (
                    <span className="rounded-full bg-brand px-2 py-0.5 text-[11px] font-bold text-white" aria-label={`${chatUnread} unread`}>
                      {chatUnread > 99 ? "99+" : chatUnread}
                    </span>
                  )}
                </Link>
              );
            })}
          </div>
        );
      })}
    </nav>
  );

  const initials = me.name.split(" ").map((p) => p[0]).join("").slice(0, 2).toUpperCase();

  return (
    <MeContext.Provider value={me}>
      <div className="min-h-screen lg:flex">
        <aside
          className={cx(
            "fixed inset-y-0 left-0 z-30 flex w-64 flex-col border-r border-line bg-white transition-transform lg:sticky lg:top-0 lg:h-screen lg:translate-x-0",
            open ? "translate-x-0" : "-translate-x-full",
          )}
        >
          <div className="flex h-16 items-center gap-2.5 border-b border-line px-5">
            <span className="grid h-8 w-8 place-items-center rounded-lg bg-brand font-display text-sm font-bold text-white">HR</span>
            <span className="flex-1 font-display text-lg font-bold">Workspace</span>
            {wide && <NotificationBell />}
          </div>
          {nav}
          <div className="flex items-center gap-3 border-t border-line p-4">
            <span className="grid h-9 w-9 shrink-0 place-items-center rounded-full bg-[#dfe6f8] text-[13px] font-bold text-brand-ink">{initials}</span>
            <div className="flex min-w-0 flex-1 flex-col">
              <span className="truncate text-sm font-semibold">{me.name}</span>
              <span className="text-xs text-muted">{roleLabel[me.role]}{me.teams.length ? ` · ${me.teams.map((t) => t.name).join(", ")}` : ""}</span>
            </div>
            <button type="button" onClick={signOut} className="rounded-lg px-2 py-1 text-xs font-semibold text-muted hover:bg-canvas hover:text-ink">
              Sign out
            </button>
          </div>
        </aside>
        {open && <button type="button" aria-label="Close menu" className="fixed inset-0 z-20 bg-black/30 lg:hidden" onClick={() => setOpen(false)} />}
        <div className="min-w-0 flex-1">
          <div className="sticky top-0 z-10 flex h-14 items-center gap-3 border-b border-line bg-white px-4 lg:hidden">
            <button type="button" onClick={() => setOpen(true)} className="rounded-lg border border-line-2 px-3 py-1.5 text-sm font-semibold">
              Menu
            </button>
            <span className="flex-1 font-display font-bold">Workspace</span>
            {!wide && <NotificationBell align="right" />}
          </div>
          <main className="mx-auto flex max-w-[1200px] flex-col gap-5 px-4 py-6 sm:px-8 sm:py-7">{children}</main>
        </div>
      </div>
    </MeContext.Provider>
  );
}

export function RoleGate({ allow, children }: { allow: (me: Me) => boolean; children: ReactNode }) {
  const me = useMe();
  if (!allow(me)) {
    return <p className="rounded-xl border border-line bg-white p-6 text-sm text-muted">You do not have access to this page.</p>;
  }
  return <>{children}</>;
}
