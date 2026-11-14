"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { FormEvent, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ChannelView, OnlineDot } from "@/components/chat-channel";
import { useMe } from "@/components/shell";
import { Button, cx, inputCls, Notice } from "@/components/ui";
import { api, Me } from "@/lib/api";
import { ago, Conversations, DM, dmTitle, TeamChannel } from "@/lib/chat";
import { useLive } from "@/lib/live";
import { avatarColor, initials } from "@/lib/tasks";

export default function ChatPage() {
  return (
    <Suspense fallback={<p className="text-sm text-muted">Loading…</p>}>
      <Chat />
    </Suspense>
  );
}

type PersonHit = { id: string; name: string; title: string; teams: string; online: boolean };
type SearchHit = { id: string; channel_id: string; where: string; author_name: string; text: string; created_at: string };

function Chat() {
  const me = useMe();
  const router = useRouter();
  const params = useSearchParams();
  const selected = params.get("c");
  const jumpTo = params.get("m");
  const [conv, setConv] = useState<Conversations | null>(null);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");
  const [search, setSearch] = useState<string | null>(null);
  const [dialog, setDialog] = useState<null | { kind: "dm" } | { kind: "channel"; teamId: string; teamName: string }>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const load = useCallback(async () => {
    try {
      setConv(await api<Conversations>("/chat/conversations"));
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);
  const reload = useCallback(() => {
    clearTimeout(timer.current);
    timer.current = setTimeout(load, 250);
  }, [load]);

  useEffect(() => {
    load();
    const t = setInterval(load, 60000); // online dots
    return () => clearInterval(t);
  }, [load]);

  useLive((e) => {
    if (["message.new", "message.delete", "read", "channel.changed", "reconnected"].includes(e.type)) reload();
  });

  // Open the first channel on wide screens when nothing is picked.
  useEffect(() => {
    if (selected || !conv || typeof window === "undefined" || window.innerWidth < 1024) return;
    const first = conv.teams[0]?.channels.find((c) => !c.archived)?.id ?? conv.dms[0]?.id;
    if (first) router.replace(`/chat?c=${first}`);
  }, [selected, conv, router]);

  function open(id: string, msg?: string) {
    setSearch(null);
    router.push(`/chat?c=${id}${msg ? `&m=${msg}` : ""}`);
  }

  return (
    <div className="-mx-4 -my-6 flex h-[calc(100dvh-3.5rem)] gap-4 p-3 sm:-mx-8 sm:-my-7 sm:p-5 lg:h-dvh">
      <aside
        aria-label="Conversations"
        className={cx(
          "min-h-0 w-full flex-col rounded-2xl border border-line bg-white lg:flex lg:w-[300px] lg:shrink-0",
          selected || search ? "hidden" : "flex",
        )}
      >
        <ConversationList
          me={me}
          conv={conv}
          error={error}
          selected={selected}
          filter={filter}
          setFilter={setFilter}
          onOpen={(id) => open(id)}
          onSearch={(q) => {
            setSearch(q);
            router.push("/chat");
          }}
          onNewDM={() => setDialog({ kind: "dm" })}
          onNewChannel={(teamId, teamName) => setDialog({ kind: "channel", teamId, teamName })}
          onStartDM={async (id) => {
            try {
              const r = await api<{ id: string }>("/chat/dms", { body: { user_ids: [id] } });
              setFilter("");
              await load();
              open(r.id);
            } catch (e) {
              setError((e as Error).message);
            }
          }}
        />
      </aside>
      <section
        aria-label={search ? "Search results" : "Conversation"}
        className={cx("min-h-0 min-w-0 flex-1 flex-col rounded-2xl border border-line bg-white", selected || search ? "flex" : "hidden lg:flex")}
      >
        {search ? (
          <SearchResults q={search} onOpen={open} onBack={() => setSearch(null)} />
        ) : selected ? (
          <ChannelView
            key={selected}
            me={me}
            channelId={selected}
            jumpTo={jumpTo}
            onJumped={() => router.replace(`/chat?c=${selected}`, { scroll: false })}
            onBack={() => router.push("/chat")}
            onChanged={reload}
          />
        ) : (
          <div className="grid flex-1 place-items-center p-8 text-center text-sm text-muted">
            {conv && !conv.teams.length && !conv.dms.length ? "You are not in a team yet. Start a direct message with anyone in the company." : "Pick a conversation."}
          </div>
        )}
      </section>
      {dialog?.kind === "dm" && (
        <NewDMDialog
          onClose={() => setDialog(null)}
          onOpened={async (id) => {
            setDialog(null);
            await load();
            open(id);
          }}
        />
      )}
      {dialog?.kind === "channel" && (
        <NewChannelDialog
          teamId={dialog.teamId}
          teamName={dialog.teamName}
          onClose={() => setDialog(null)}
          onCreated={async (id) => {
            setDialog(null);
            await load();
            open(id);
          }}
        />
      )}
    </div>
  );
}

function PlusIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" aria-hidden="true">
      <path d="M12 5v14M5 12h14" />
    </svg>
  );
}

function UnreadBadge({ n, mention }: { n: number; mention?: boolean }) {
  if (!n) return null;
  return (
    <span className={cx("rounded-full px-1.5 py-0.5 text-[11px] font-bold leading-none text-white", mention ? "bg-bad" : "bg-brand")} aria-label={`${n} unread${mention ? ", you were mentioned" : ""}`}>
      {mention ? "@ " : ""}
      {n > 99 ? "99+" : n}
    </span>
  );
}

function ConversationList({
  me,
  conv,
  error,
  selected,
  filter,
  setFilter,
  onOpen,
  onSearch,
  onNewDM,
  onNewChannel,
  onStartDM,
}: {
  me: Me;
  conv: Conversations | null;
  error: string;
  selected: string | null;
  filter: string;
  setFilter: (v: string) => void;
  onOpen: (id: string) => void;
  onSearch: (q: string) => void;
  onNewDM: () => void;
  onNewChannel: (teamId: string, teamName: string) => void;
  onStartDM: (userId: string) => void;
}) {
  const [people, setPeople] = useState<PersonHit[]>([]);
  const [showArchived, setShowArchived] = useState<Record<string, boolean>>({});
  const f = filter.trim().toLowerCase();

  useEffect(() => {
    if (f.length < 2) {
      setPeople([]);
      return;
    }
    const t = setTimeout(() => {
      api<{ people: PersonHit[] }>(`/chat/people?q=${encodeURIComponent(f)}`)
        .then((d) => setPeople(d.people))
        .catch(() => setPeople([]));
    }, 250);
    return () => clearTimeout(t);
  }, [f]);

  const dmByPerson = useMemo(() => {
    const m = new Map<string, DM>();
    conv?.dms.forEach((d) => d.members.length === 1 && m.set(d.members[0].id, d));
    return m;
  }, [conv]);

  if (!conv) {
    return <div className="p-4 text-sm text-muted">{error ? <Notice tone="bad">{error}</Notice> : "Loading…"}</div>;
  }

  const channelMatch = (c: TeamChannel) => !f || c.name.includes(f) || c.topic.toLowerCase().includes(f);
  const dmMatch = (d: DM) => !f || dmTitle(d.members).toLowerCase().includes(f);
  const dms = conv.dms.filter(dmMatch);
  const newPeople = people.filter((p) => !dmByPerson.has(p.id));

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <form
        className="p-3"
        role="search"
        onSubmit={(e) => {
          e.preventDefault();
          if (filter.trim().length >= 2) onSearch(filter.trim());
        }}
      >
        <label className="flex h-10 items-center gap-2 rounded-[10px] border border-line-2 px-3 focus-within:border-brand">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#5B6170" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16z M21 21l-4.3-4.3" />
          </svg>
          <input
            type="search"
            aria-label="Search people, channels and messages"
            placeholder="Search people and channels"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="min-w-0 flex-1 border-0 text-sm outline-none"
          />
        </label>
        {f.length >= 2 && (
          <button type="submit" className="mt-2 w-full rounded-lg bg-canvas px-3 py-2 text-left text-[13px] font-semibold text-brand-ink hover:bg-brand-soft">
            Search messages for “{filter.trim()}”
          </button>
        )}
      </form>
      <nav aria-label="Channels and direct messages" className="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto px-2 pb-4">
        {conv.teams.map((t) => {
          const live = t.channels.filter((c) => !c.archived && channelMatch(c));
          const archived = t.channels.filter((c) => c.archived && channelMatch(c));
          if (f && !live.length && !archived.length) return null;
          return (
            <div key={t.id} className="flex flex-col gap-0.5">
              <div className="flex items-center justify-between px-2 pb-1">
                <span className="text-[11px] font-semibold uppercase tracking-wider text-muted">{t.name} team channels</span>
                <button type="button" aria-label={`New channel in ${t.name}`} title="New channel" onClick={() => onNewChannel(t.id, t.name)} className="grid h-7 w-7 place-items-center rounded-md bg-canvas text-ink-2 hover:bg-brand-soft">
                  <PlusIcon />
                </button>
              </div>
              {live.map((c) => (
                <ChannelLink key={c.id} c={c} active={selected === c.id} onOpen={onOpen} />
              ))}
              {archived.length > 0 && (
                <>
                  <button type="button" onClick={() => setShowArchived((s) => ({ ...s, [t.id]: !s[t.id] }))} className="px-2.5 py-1 text-left text-xs font-semibold text-muted hover:text-ink" aria-expanded={!!showArchived[t.id]}>
                    {showArchived[t.id] ? "Hide" : "Show"} archived ({archived.length})
                  </button>
                  {showArchived[t.id] && archived.map((c) => <ChannelLink key={c.id} c={c} active={selected === c.id} onOpen={onOpen} />)}
                </>
              )}
            </div>
          );
        })}
        {!f && <p className="-mt-3 px-2.5 text-xs text-muted">{conv.teams.length ? "Other teams' channels are private to them." : "You are not in a team, so you have no team channels."}</p>}

        <div className="flex flex-col gap-0.5">
          <div className="flex items-center justify-between px-2 pb-1">
            <span className="text-[11px] font-semibold uppercase tracking-wider text-muted">Direct messages · anyone</span>
            <button type="button" aria-label="New message" title="New message" onClick={onNewDM} className="grid h-7 w-7 place-items-center rounded-md bg-canvas text-ink-2 hover:bg-brand-soft">
              <PlusIcon />
            </button>
          </div>
          {dms.map((d) => (
            <DMLink key={d.id} d={d} active={selected === d.id} onOpen={onOpen} />
          ))}
          {!dms.length && !f && <p className="px-2.5 text-xs text-muted">No conversations yet. Press + to message anyone in the company.</p>}
          {newPeople.length > 0 && (
            <>
              <span className="mt-3 px-2 pb-1 text-[11px] font-semibold uppercase tracking-wider text-muted">Start a conversation</span>
              {newPeople.map((p) => (
                <button key={p.id} type="button" onClick={() => onStartDM(p.id)} className="flex min-h-[42px] items-center gap-2.5 rounded-lg px-2.5 text-left hover:bg-canvas">
                  <PersonAvatar id={p.id} name={p.name} online={p.online} />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-sm font-medium">{p.name}</span>
                    <span className="truncate text-xs text-muted">{[p.title, p.teams].filter(Boolean).join(" · ")}</span>
                  </span>
                </button>
              ))}
            </>
          )}
          {f && !dms.length && !newPeople.length && f.length >= 2 && <p className="px-2.5 text-xs text-muted">Nobody matches “{filter.trim()}”.</p>}
        </div>
      </nav>
      <span className="sr-only">Signed in as {me.name}</span>
    </div>
  );
}

function ChannelLink({ c, active, onOpen }: { c: TeamChannel; active: boolean; onOpen: (id: string) => void }) {
  return (
    <a
      href={`/chat?c=${c.id}`}
      onClick={(e) => {
        e.preventDefault();
        onOpen(c.id);
      }}
      aria-current={active ? "page" : undefined}
      className={cx(
        "flex min-h-[38px] items-center gap-2 rounded-lg px-2.5 text-sm",
        active ? "bg-brand-soft font-semibold text-brand-ink" : c.unread ? "font-semibold text-ink hover:bg-canvas" : "text-ink hover:bg-canvas",
        c.archived && "text-muted",
      )}
    >
      <span className="text-muted" aria-hidden="true">
        #
      </span>
      <span className="min-w-0 flex-1 truncate">{c.name}</span>
      <UnreadBadge n={c.unread} mention={c.mentioned > 0} />
    </a>
  );
}

function PersonAvatar({ id, name, online }: { id: string; name: string; online: boolean }) {
  return (
    <span className="relative shrink-0">
      <span className="grid h-7 w-7 place-items-center rounded-full text-[10px] font-bold text-white" style={{ background: avatarColor(id) }} aria-hidden="true">
        {initials(name)}
      </span>
      <OnlineDot online={online} className="absolute -bottom-0.5 -right-0.5" />
    </span>
  );
}

function DMLink({ d, active, onOpen }: { d: DM; active: boolean; onOpen: (id: string) => void }) {
  const one = d.members.length === 1 ? d.members[0] : null;
  return (
    <a
      href={`/chat?c=${d.id}`}
      onClick={(e) => {
        e.preventDefault();
        onOpen(d.id);
      }}
      aria-current={active ? "page" : undefined}
      className={cx("flex min-h-[44px] items-center gap-2.5 rounded-lg px-2.5", active ? "bg-brand-soft" : "hover:bg-canvas")}
    >
      {one ? (
        <PersonAvatar id={one.id} name={one.name} online={one.online} />
      ) : (
        <span className="grid h-7 w-7 shrink-0 place-items-center rounded-full bg-[#3A3F4A] text-[10px] font-bold text-white" aria-hidden="true">
          {d.members.length + 1}
        </span>
      )}
      <span className="flex min-w-0 flex-1 flex-col">
        <span className={cx("truncate text-sm", d.unread || active ? "font-semibold" : "font-medium", active && "text-brand-ink")}>{dmTitle(d.members)}</span>
        <span className="truncate text-xs text-muted">{one ? (one.active ? one.teams || one.title : "Deactivated") : "Group message"}</span>
      </span>
      <UnreadBadge n={d.unread} />
    </a>
  );
}

function SearchResults({ q, onOpen, onBack }: { q: string; onOpen: (channel: string, msg: string) => void; onBack: () => void }) {
  const [hits, setHits] = useState<SearchHit[] | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    setHits(null);
    api<{ results: SearchHit[] }>(`/chat/search?q=${encodeURIComponent(q)}`)
      .then((d) => setHits(d.results))
      .catch((e) => setError((e as Error).message));
  }, [q]);
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex items-center gap-3 border-b border-line px-5 py-3">
        <Button size="sm" onClick={onBack}>
          Back
        </Button>
        <h1 className="min-w-0 truncate text-lg font-bold">Messages with “{q}”</h1>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        {error && <Notice tone="bad">{error}</Notice>}
        {!hits && !error && <p className="p-4 text-sm text-muted">Searching…</p>}
        {hits?.length === 0 && <p className="p-6 text-center text-sm text-muted">No messages found in your channels and conversations.</p>}
        <ul className="flex flex-col">
          {hits?.map((h) => (
            <li key={h.id}>
              <button type="button" onClick={() => onOpen(h.channel_id, h.id)} className="flex w-full flex-col gap-1 rounded-xl px-3 py-3 text-left hover:bg-canvas">
                <span className="text-xs text-muted">
                  <span className="font-semibold text-ink-2">{h.where}</span> · {ago(h.created_at)}
                </span>
                <span className="text-sm">
                  <strong className="font-semibold">{h.author_name}:</strong> {h.text}
                </span>
              </button>
            </li>
          ))}
        </ul>
        {hits && hits.length >= 40 && <p className="p-3 text-center text-xs text-muted">Showing the 40 newest matches. Add more words to narrow it down.</p>}
      </div>
    </div>
  );
}

function NewDMDialog({ onClose, onOpened }: { onClose: () => void; onOpened: (id: string) => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const [q, setQ] = useState("");
  const [people, setPeople] = useState<PersonHit[]>([]);
  const [picked, setPicked] = useState<PersonHit[]>([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    ref.current?.showModal();
  }, []);
  useEffect(() => {
    const t = setTimeout(() => {
      api<{ people: PersonHit[] }>(`/chat/people?q=${encodeURIComponent(q.trim())}`)
        .then((d) => setPeople(d.people))
        .catch(() => setPeople([]));
    }, 200);
    return () => clearTimeout(t);
  }, [q]);
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!picked.length) return;
    setBusy(true);
    setErr("");
    try {
      const r = await api<{ id: string }>("/chat/dms", { body: { user_ids: picked.map((p) => p.id) } });
      onOpened(r.id);
    } catch (e) {
      setErr((e as Error).message);
      setBusy(false);
    }
  }
  const toggle = (p: PersonHit) =>
    setPicked((cur) => (cur.some((x) => x.id === p.id) ? cur.filter((x) => x.id !== p.id) : cur.length >= 8 ? cur : [...cur, p]));
  return (
    <dialog ref={ref} onClose={onClose} className="m-auto w-[min(520px,calc(100vw-32px))] rounded-2xl border border-line p-0 backdrop:bg-black/40">
      <form onSubmit={submit} className="flex max-h-[80vh] flex-col gap-4 p-6">
        <div className="flex items-baseline justify-between">
          <h2 className="font-display text-xl font-bold">New message</h2>
          <button type="button" className="text-sm font-semibold text-muted hover:text-ink" onClick={() => ref.current?.close()}>
            Close
          </button>
        </div>
        <p className="text-[13px] text-muted">Pick one person, or up to 8 for a group message. Anyone in the company can be messaged.</p>
        {picked.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {picked.map((p) => (
              <button key={p.id} type="button" onClick={() => toggle(p)} className="inline-flex items-center gap-1.5 rounded-full bg-brand-soft px-3 py-1 text-[13px] font-semibold text-brand-ink" aria-label={`Remove ${p.name}`}>
                {p.name}
                <span aria-hidden="true">×</span>
              </button>
            ))}
          </div>
        )}
        <input autoFocus aria-label="Find people" className={inputCls} placeholder="Type a name or job title" value={q} onChange={(e) => setQ(e.target.value)} />
        <ul className="-mx-2 min-h-[120px] flex-1 overflow-y-auto">
          {people.map((p) => {
            const on = picked.some((x) => x.id === p.id);
            return (
              <li key={p.id}>
                <label className={cx("flex cursor-pointer items-center gap-3 rounded-lg px-2 py-2", on ? "bg-brand-soft" : "hover:bg-canvas")}>
                  <input type="checkbox" checked={on} onChange={() => toggle(p)} className="h-4 w-4 accent-[#2f5bd3]" disabled={!on && picked.length >= 8} />
                  <PersonAvatar id={p.id} name={p.name} online={p.online} />
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate text-sm font-semibold">{p.name}</span>
                    <span className="truncate text-xs text-muted">{[p.title, p.teams].filter(Boolean).join(" · ")}</span>
                  </span>
                </label>
              </li>
            );
          })}
          {!people.length && <li className="p-4 text-center text-sm text-muted">Nobody matches.</li>}
        </ul>
        {err && <Notice tone="bad">{err}</Notice>}
        <Button type="submit" variant="primary" disabled={busy || !picked.length}>
          {picked.length > 1 ? `Start group message (${picked.length + 1} people)` : "Open conversation"}
        </Button>
      </form>
    </dialog>
  );
}

function slugPreview(s: string) {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
    .replace(/-+$/, "");
}

function NewChannelDialog({ teamId, teamName, onClose, onCreated }: { teamId: string; teamName: string; onClose: () => void; onCreated: (id: string) => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const [name, setName] = useState("");
  const [topic, setTopic] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    ref.current?.showModal();
  }, []);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      const r = await api<{ id: string }>(`/chat/teams/${teamId}/channels`, { body: { name, topic } });
      onCreated(r.id);
    } catch (e) {
      setErr((e as Error).message);
      setBusy(false);
    }
  }
  const slug = slugPreview(name);
  return (
    <dialog ref={ref} onClose={onClose} className="m-auto w-[min(520px,calc(100vw-32px))] rounded-2xl border border-line p-0 backdrop:bg-black/40">
      <form onSubmit={submit} className="flex flex-col gap-4 p-6">
        <div className="flex items-baseline justify-between">
          <h2 className="font-display text-xl font-bold">New {teamName} channel</h2>
          <button type="button" className="text-sm font-semibold text-muted hover:text-ink" onClick={() => ref.current?.close()}>
            Close
          </button>
        </div>
        <p className="text-[13px] text-muted">Every member of the {teamName} team can read and post in it. Nobody outside the team can see it.</p>
        <label className="flex flex-col gap-1.5 text-[13px] font-semibold text-ink-2">
          Name
          <input autoFocus required className={inputCls} value={name} maxLength={60} onChange={(e) => setName(e.target.value)} placeholder="release-notes" />
          <span className="text-xs font-normal text-muted">{slug ? `It will be #${slug}` : "Letters, numbers and dashes."}</span>
        </label>
        <label className="flex flex-col gap-1.5 text-[13px] font-semibold text-ink-2">
          Topic (optional)
          <input className={inputCls} value={topic} maxLength={250} onChange={(e) => setTopic(e.target.value)} placeholder="What this channel is for" />
        </label>
        {err && <Notice tone="bad">{err}</Notice>}
        <Button type="submit" variant="primary" disabled={busy || !slug}>
          Create channel
        </Button>
      </form>
    </dialog>
  );
}
