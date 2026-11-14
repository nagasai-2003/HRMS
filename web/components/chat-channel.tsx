"use client";

import Link from "next/link";
import { FormEvent, Fragment, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api, Me } from "@/lib/api";
import { ChannelInfo, dayDivider, dayKey, dmTitle, fromBody, idNum, Message, PersonRef, timeOf, toBody } from "@/lib/chat";
import { useLive, useLiveSend } from "@/lib/live";
import { avatarColor, initials, MyTask } from "@/lib/tasks";
import { MentionInput, MentionInputHandle } from "./mention-input";
import { RichText } from "./rich-text";
import { Button, cx, inputCls, Notice } from "./ui";

const nearBottomPx = 140;

function Avatar({ id, name, size = 36, square = true }: { id: string; name: string; size?: number; square?: boolean }) {
  return (
    <span
      aria-hidden="true"
      className={cx("grid shrink-0 place-items-center font-bold text-white", square ? "rounded-[10px]" : "rounded-full")}
      style={{ width: size, height: size, background: avatarColor(id), fontSize: size > 30 ? 12 : 10 }}
    >
      {initials(name)}
    </span>
  );
}

export function OnlineDot({ online, className }: { online: boolean; className?: string }) {
  return (
    <span
      className={cx("block h-2.5 w-2.5 rounded-full border-2 border-white", online ? "bg-[#15803D]" : "bg-[#8A909C]", className)}
      title={online ? "Online" : "Offline"}
    />
  );
}

type Props = {
  me: Me;
  channelId: string;
  jumpTo: string | null;
  onJumped: () => void;
  onBack: () => void;
  onChanged: () => void;
};

/** One conversation: header, messages and the composer. */
export function ChannelView({ me, channelId, jumpTo, onJumped, onBack, onChanged }: Props) {
  const [info, setInfo] = useState<ChannelInfo | null>(null);
  const [msgs, setMsgs] = useState<Message[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [error, setError] = useState("");
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [newBelow, setNewBelow] = useState(false);
  const [typing, setTyping] = useState<Record<string, { name: string; until: number }>>({});
  const [highlight, setHighlight] = useState<string | null>(null);
  const [showMembers, setShowMembers] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const firstUnread = useRef<string | null>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  const keepFrom = useRef<number | null>(null);
  const lastMarked = useRef(0);

  const loadInfo = useCallback(async () => {
    try {
      setInfo(await api<ChannelInfo>(`/chat/channels/${channelId}`));
    } catch (e) {
      setError((e as Error).message);
    }
  }, [channelId]);

  // First load: details and the newest page of messages.
  useEffect(() => {
    let gone = false;
    (async () => {
      try {
        const [i, page] = await Promise.all([
          api<ChannelInfo>(`/chat/channels/${channelId}`),
          api<{ messages: Message[]; has_more: boolean }>(`/chat/channels/${channelId}/messages`),
        ]);
        if (gone) return;
        const first = page.messages.find((m) => idNum(m.id) > idNum(i.last_read_id) && m.author.id !== me.id);
        firstUnread.current = first?.id ?? null;
        setInfo(i);
        setMsgs(page.messages);
        setHasMore(page.has_more);
        stick.current = true;
      } catch (e) {
        if (!gone) setError((e as Error).message);
      }
    })();
    return () => {
      gone = true;
    };
  }, [channelId, me.id]);

  const upsert = useCallback((m: Message) => {
    setMsgs((cur) => {
      const i = cur.findIndex((x) => x.id === m.id);
      if (i >= 0) {
        const next = cur.slice();
        next[i] = { ...m, tasks: m.tasks ?? (m.body === cur[i].body ? cur[i].tasks : null) };
        return next;
      }
      if (cur.length && idNum(m.id) < idNum(cur[cur.length - 1].id)) {
        return [...cur, m].sort((a, b) => idNum(a.id) - idNum(b.id));
      }
      return [...cur, m];
    });
  }, []);

  // Task cards depend on who is reading, so ask for them.
  const fillCards = useCallback(
    async (m: Message) => {
      if (m.deleted || !m.task_refs.length || m.tasks) return;
      try {
        upsert(await api<Message>(`/chat/messages/${m.id}`));
      } catch {
        /* the message stays without cards */
      }
    },
    [upsert],
  );

  const catchUp = useCallback(async () => {
    const last = msgs[msgs.length - 1];
    try {
      if (!last) {
        const page = await api<{ messages: Message[]; has_more: boolean }>(`/chat/channels/${channelId}/messages`);
        setMsgs(page.messages);
        setHasMore(page.has_more);
      } else {
        const page = await api<{ messages: Message[] }>(`/chat/channels/${channelId}/messages?after=${last.id}`);
        page.messages.forEach(upsert);
      }
    } catch {
      /* next reconnect tries again */
    }
  }, [channelId, msgs, upsert]);

  useLive((e) => {
    if (e.type === "reconnected") {
      catchUp();
      loadInfo();
      return;
    }
    if (e.data?.channel_id !== channelId) return;
    switch (e.type) {
      case "message.new":
      case "message.edit":
      case "message.delete": {
        const m = e.data.message as Message;
        const el = scroller.current;
        const near = !el || el.scrollHeight - el.scrollTop - el.clientHeight < nearBottomPx;
        if (e.type === "message.new") {
          if (near || m.author.id === me.id) stick.current = true;
          else setNewBelow(true);
          setTyping((t) => {
            const { [m.author.id]: _, ...rest } = t;
            return rest;
          });
        }
        upsert(m);
        fillCards(m);
        break;
      }
      case "typing": {
        const u = e.data.user as PersonRef;
        setTyping((t) => ({ ...t, [u.id]: { name: u.name, until: Date.now() + 4000 } }));
        break;
      }
      case "channel.changed":
        loadInfo();
        onChanged();
        break;
    }
  });

  // Typing notes fade after 4 seconds.
  useEffect(() => {
    if (!Object.keys(typing).length) return;
    const t = setInterval(() => {
      setTyping((cur) => {
        const now = Date.now();
        const next = Object.fromEntries(Object.entries(cur).filter(([, v]) => v.until > now));
        return Object.keys(next).length === Object.keys(cur).length ? cur : next;
      });
    }, 1000);
    return () => clearInterval(t);
  }, [typing]);

  // Scrolling: stay at the bottom, keep place when older messages load.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    if (keepFrom.current !== null) {
      el.scrollTop = el.scrollHeight - keepFrom.current;
      keepFrom.current = null;
      return;
    }
    if (stick.current) {
      const divider = firstUnread.current ? el.querySelector<HTMLElement>(`[data-msg="${firstUnread.current}"]`) : null;
      if (divider && msgs.length && !jumpTo) {
        el.scrollTop = Math.max(0, divider.offsetTop - 80);
      } else {
        el.scrollTop = el.scrollHeight;
      }
      stick.current = false;
    }
  }, [msgs, jumpTo]);

  // Mark as read when the newest message is on screen and the tab is visible.
  const markRead = useCallback(() => {
    const el = scroller.current;
    const last = msgs[msgs.length - 1];
    if (!el || !last || document.visibilityState !== "visible") return;
    if (el.scrollHeight - el.scrollTop - el.clientHeight > nearBottomPx) return;
    if (idNum(last.id) <= lastMarked.current) return;
    lastMarked.current = idNum(last.id);
    setNewBelow(false);
    api(`/chat/channels/${channelId}/read`, { body: { last_read_id: last.id } }).catch(() => {
      lastMarked.current = 0;
    });
  }, [channelId, msgs]);

  useEffect(() => {
    const t = setTimeout(markRead, 500);
    document.addEventListener("visibilitychange", markRead);
    return () => {
      clearTimeout(t);
      document.removeEventListener("visibilitychange", markRead);
    };
  }, [markRead]);

  async function loadOlder() {
    if (!msgs.length || loadingOlder) return null;
    setLoadingOlder(true);
    try {
      const page = await api<{ messages: Message[]; has_more: boolean }>(`/chat/channels/${channelId}/messages?before=${msgs[0].id}`);
      const el = scroller.current;
      if (el) keepFrom.current = el.scrollHeight - el.scrollTop;
      setMsgs((cur) => [...page.messages.filter((m) => !cur.some((c) => c.id === m.id)), ...cur]);
      setHasMore(page.has_more);
      return page;
    } catch (e) {
      setError((e as Error).message);
      return null;
    } finally {
      setLoadingOlder(false);
    }
  }

  // Opening a link to one message (from search or a notification).
  const jumping = useRef(false);
  useEffect(() => {
    if (!jumpTo || !info || jumping.current) return;
    const el = scroller.current?.querySelector<HTMLElement>(`[data-msg="${jumpTo}"]`);
    if (el) {
      el.scrollIntoView({ block: "center" });
      setHighlight(jumpTo);
      setTimeout(() => setHighlight(null), 2500);
      onJumped();
      return;
    }
    if (!msgs.length || !hasMore || idNum(msgs[0].id) <= idNum(jumpTo)) {
      onJumped();
      return;
    }
    jumping.current = true;
    loadOlder().finally(() => {
      jumping.current = false;
    });
  }, [jumpTo, info, msgs, hasMore]); // eslint-disable-line react-hooks/exhaustive-deps

  if (error && !info) {
    return (
      <div className="flex flex-1 flex-col gap-3 p-6">
        <button type="button" onClick={onBack} className="self-start text-sm font-semibold text-brand lg:hidden">
          Back to conversations
        </button>
        <Notice tone="bad">{error}</Notice>
      </div>
    );
  }
  if (!info) return <div className="grid flex-1 place-items-center text-sm text-muted">Loading…</div>;

  const ch = info.channel;
  const others = info.members.filter((m) => m.id !== me.id);
  const title = ch.kind === "team" ? `# ${ch.name}` : dmTitle(others);
  const subtitle =
    ch.kind === "team"
      ? `${info.members.length} members · private to the ${ch.team?.name} team${ch.archived ? " · archived" : ""}`
      : others.length === 1
        ? `${others[0].title || "Direct message"} · ${!others[0].active ? "account deactivated" : others[0].online ? "online" : "offline"}`
        : `Group message · ${info.members.length} people`;
  const typers = Object.values(typing).map((t) => t.name);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-3 sm:px-5">
        <div className="flex min-w-0 items-center gap-3">
          <button type="button" onClick={onBack} aria-label="Back to conversations" className="grid h-9 w-9 place-items-center rounded-lg border border-line-2 lg:hidden">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M15 18l-6-6 6-6" />
            </svg>
          </button>
          {ch.kind === "dm" && others.length === 1 && (
            <span className="relative">
              <Avatar id={others[0].id} name={others[0].name} size={36} square={false} />
              <OnlineDot online={others[0].online} className="absolute -bottom-0.5 -right-0.5" />
            </span>
          )}
          <div className="flex min-w-0 flex-col">
            <h1 className="truncate text-lg font-bold">{title}</h1>
            <span className="truncate text-xs text-muted">{subtitle}</span>
          </div>
        </div>
        <div className="relative flex gap-2">
          <Button size="sm" aria-expanded={showMembers} onClick={() => setShowMembers((s) => !s)}>
            Members
          </Button>
          {(info.can_rename || info.can_archive || (ch.archived && info.can_moderate)) && (
            <Button size="sm" onClick={() => setShowSettings(true)}>
              Channel settings
            </Button>
          )}
          {showMembers && <MembersPopover members={info.members} me={me.id} onClose={() => setShowMembers(false)} />}
        </div>
        {ch.topic && <p className="w-full text-[13px] text-ink-2">{ch.topic}</p>}
      </header>

      <div ref={scroller} onScroll={markRead} className="relative min-h-0 flex-1 overflow-y-auto px-4 py-4 sm:px-5" aria-live="polite" aria-relevant="additions">
        {hasMore && (
          <div className="mb-4 flex justify-center">
            <Button size="sm" onClick={loadOlder} disabled={loadingOlder}>
              {loadingOlder ? "Loading…" : "Load older messages"}
            </Button>
          </div>
        )}
        {!hasMore && (
          <div className="mb-5 flex flex-col gap-1 border-b border-line pb-4">
            <span className="text-sm font-semibold">{ch.kind === "team" ? `This is the start of #${ch.name}.` : `This is the start of your conversation with ${dmTitle(others)}.`}</span>
            <span className="text-[13px] text-muted">
              {ch.kind === "team"
                ? `Only members of the ${ch.team?.name} team can read it. Type @ to mention someone, or a task key like ${ch.team?.key}-12 to link a task.`
                : "Only the people in this conversation can read it."}
            </span>
          </div>
        )}
        {msgs.length === 0 && <p className="py-10 text-center text-sm text-muted">No messages yet. Say hello.</p>}
        <MessageList
          msgs={msgs}
          me={me}
          info={info}
          firstUnread={firstUnread.current}
          highlight={highlight}
          onChange={upsert}
          people={others.filter((m) => m.active)}
        />
        {newBelow && (
          <button
            type="button"
            onClick={() => {
              const el = scroller.current;
              if (el) el.scrollTop = el.scrollHeight;
              setNewBelow(false);
            }}
            className="sticky bottom-2 left-1/2 mx-auto mt-2 block -translate-x-0 rounded-full bg-brand px-4 py-2 text-xs font-semibold text-white shadow-lg"
          >
            New messages below
          </button>
        )}
      </div>

      <div className="h-6 px-5 text-xs text-muted" aria-live="polite">
        {typers.length === 1 && `${typers[0]} is typing…`}
        {typers.length === 2 && `${typers[0]} and ${typers[1]} are typing…`}
        {typers.length > 2 && "Several people are typing…"}
      </div>
      {info.can_post ? (
        <Composer
          key={channelId}
          channelId={channelId}
          placeholder={ch.kind === "team" ? `Message #${ch.name}` : `Message ${dmTitle(others)}`}
          people={others.filter((m) => m.active)}
          onSent={(m) => {
            stick.current = true;
            upsert(m);
          }}
        />
      ) : (
        <div className="border-t border-line px-5 py-4">
          <Notice tone="warn">This channel is archived, so nobody can post in it. {info.can_moderate ? "Bring it back from Channel settings." : "A team manager can bring it back."}</Notice>
        </div>
      )}
      {showSettings && (
        <ChannelSettings
          info={info}
          onClose={() => setShowSettings(false)}
          onSaved={(i) => {
            setInfo(i);
            onChanged();
          }}
        />
      )}
    </div>
  );
}

function MessageList({
  msgs,
  me,
  info,
  firstUnread,
  highlight,
  onChange,
  people,
}: {
  msgs: Message[];
  me: Me;
  info: ChannelInfo;
  firstUnread: string | null;
  highlight: string | null;
  onChange: (m: Message) => void;
  people: PersonRef[];
}) {
  const now = useMemo(() => new Date(), [msgs.length]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <ol className="flex flex-col">
      {msgs.map((m, i) => {
        const prev = msgs[i - 1];
        const newDay = !prev || dayKey(prev.created_at) !== dayKey(m.created_at);
        const unreadLine = firstUnread === m.id;
        const compact =
          !newDay && !unreadLine && prev && prev.author.id === m.author.id && !prev.deleted && Date.parse(m.created_at) - Date.parse(prev.created_at) < 5 * 60000;
        return (
          <Fragment key={m.id}>
            {newDay && (
              <li className="my-3 flex items-center gap-3 text-xs font-semibold text-muted" aria-hidden="false">
                <span className="h-px flex-1 bg-line" />
                {dayDivider(m.created_at, now)}
                <span className="h-px flex-1 bg-line" />
              </li>
            )}
            {unreadLine && (
              <li className="my-2 flex items-center gap-3 text-xs font-bold text-bad">
                <span className="h-px flex-1 bg-[#f3c2bd]" />
                New
                <span className="h-px flex-1 bg-[#f3c2bd]" />
              </li>
            )}
            <MessageRow m={m} me={me} info={info} compact={!!compact} highlight={highlight === m.id} onChange={onChange} people={people} />
          </Fragment>
        );
      })}
    </ol>
  );
}

function MessageRow({
  m,
  me,
  info,
  compact,
  highlight,
  onChange,
  people,
}: {
  m: Message;
  me: Me;
  info: ChannelInfo;
  compact: boolean;
  highlight: boolean;
  onChange: (m: Message) => void;
  people: PersonRef[];
}) {
  const [editing, setEditing] = useState<{ text: string; picked: PersonRef[] } | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const mine = m.author.id === me.id;
  const canEdit = mine && !m.deleted && info.can_post;
  const canDelete = !m.deleted && (mine || info.can_moderate);

  async function save() {
    if (!editing) return;
    setBusy(true);
    setErr("");
    try {
      onChange(await api<Message>(`/chat/messages/${m.id}`, { method: "PATCH", body: { body: toBody(editing.text, editing.picked) } }));
      setEditing(null);
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!confirm(mine ? "Delete this message? Everyone will see that a message was deleted." : `Delete ${m.author.name}'s message? Everyone will see that a message was deleted.`)) return;
    try {
      await api(`/chat/messages/${m.id}`, { method: "DELETE" });
      onChange({ ...m, body: "", mentions: [], task_refs: [], tasks: [], deleted: true });
    } catch (e) {
      alert((e as Error).message);
    }
  }

  return (
    <li
      data-msg={m.id}
      className={cx(
        "group relative -mx-2 flex gap-3 rounded-lg px-2 transition-colors hover:bg-[#f8f9fb] focus-within:bg-[#f8f9fb]",
        compact ? "py-0.5" : "mt-2 py-1.5",
        highlight && "bg-warn-soft hover:bg-warn-soft",
      )}
    >
      <span className="w-9 shrink-0 pt-0.5">
        {compact ? (
          <span className="hidden pt-1 text-[10px] text-muted group-hover:block">{timeOf(m.created_at)}</span>
        ) : (
          <Avatar id={m.author.id} name={m.author.name} />
        )}
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        {!compact && (
          <span className="text-sm">
            <strong className="font-semibold">{m.author.name}</strong> <span className="text-xs text-muted">{timeOf(m.created_at)}</span>
          </span>
        )}
        {m.deleted ? (
          <span className="text-sm italic text-muted">This message was deleted.</span>
        ) : editing ? (
          <div className="flex flex-col gap-2">
            <MentionInput
              label="Edit message"
              value={editing.text}
              onChange={(text) => setEditing({ ...editing, text })}
              picked={editing.picked}
              onPick={(picked) => setEditing({ ...editing, picked })}
              people={people}
              onSubmit={save}
              onEscape={() => setEditing(null)}
            />
            {err && <span className="text-xs text-bad">{err}</span>}
            <div className="flex gap-2">
              <Button size="sm" variant="primary" onClick={save} disabled={busy || !editing.text.trim()}>
                Save
              </Button>
              <Button size="sm" onClick={() => setEditing(null)}>
                Cancel
              </Button>
              <span className="self-center text-xs text-muted">Enter saves · Esc cancels</span>
            </div>
          </div>
        ) : (
          <p className="text-sm leading-relaxed text-[#2a2e36]">
            <RichText body={m.body} mentions={m.mentions} me={me.id} />
            {m.edited && <span className="ml-1 text-xs text-muted">(edited)</span>}
          </p>
        )}
        {!m.deleted && !editing && m.tasks && m.tasks.length > 0 && (
          <div className="flex flex-col gap-1.5">
            {m.tasks.map((t) => (
              <Link key={t.id} href={`/tasks/${t.id}`} className="flex max-w-[440px] items-center gap-2.5 rounded-[10px] border border-line bg-[#f8f9fb] px-3 py-2 text-ink hover:border-line-2">
                <span className="text-xs font-bold text-muted">{t.key}</span>
                <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{t.title}</span>
                <span className="inline-flex items-center gap-1.5 whitespace-nowrap rounded-full bg-white px-2 py-0.5 text-[11px] font-semibold text-ink-2 ring-1 ring-line">
                  <span className="h-2 w-2 rounded-full" style={{ background: t.status_color }} />
                  {t.status_name}
                </span>
              </Link>
            ))}
          </div>
        )}
      </div>
      {(canEdit || canDelete) && !editing && (
        <div className="absolute -top-3 right-2 hidden gap-1 rounded-lg border border-line bg-white p-0.5 shadow-sm group-focus-within:flex group-hover:flex">
          {canEdit && (
            <button type="button" className="rounded-md px-2 py-1 text-xs font-semibold text-ink-2 hover:bg-canvas" onClick={() => setEditing(fromBody(m.body, m.mentions))}>
              Edit
            </button>
          )}
          {canDelete && (
            <button type="button" className="rounded-md px-2 py-1 text-xs font-semibold text-bad hover:bg-bad-soft" onClick={remove}>
              Delete
            </button>
          )}
        </div>
      )}
    </li>
  );
}

function Composer({
  channelId,
  placeholder,
  people,
  onSent,
}: {
  channelId: string;
  placeholder: string;
  people: PersonRef[];
  onSent: (m: Message) => void;
}) {
  const draftKey = `chat-draft:${channelId}`;
  const [text, setText] = useState(() => {
    try {
      return sessionStorage.getItem(draftKey) ?? "";
    } catch {
      return "";
    }
  });
  const [picked, setPicked] = useState<PersonRef[]>([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [taskPicker, setTaskPicker] = useState(false);
  const input = useRef<MentionInputHandle>(null);
  const send = useLiveSend();
  const lastTyping = useRef(0);

  useEffect(() => {
    try {
      if (text) sessionStorage.setItem(draftKey, text);
      else sessionStorage.removeItem(draftKey);
    } catch {
      /* drafts are a convenience */
    }
  }, [text, draftKey]);

  async function submit(e?: FormEvent) {
    e?.preventDefault();
    const body = toBody(text.trim(), picked);
    if (!body || busy) return;
    setBusy(true);
    setErr("");
    try {
      const m = await api<Message>(`/chat/channels/${channelId}/messages`, { body: { body } });
      setText("");
      setPicked([]);
      onSent(m);
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
      input.current?.focus();
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-2 border-t border-line px-4 pb-4 pt-3 sm:px-5">
      <MentionInput
        ref={input}
        label="Message"
        placeholder={placeholder}
        value={text}
        onChange={setText}
        picked={picked}
        onPick={setPicked}
        people={people}
        onSubmit={submit}
        onTyping={() => {
          if (Date.now() - lastTyping.current > 2500) {
            lastTyping.current = Date.now();
            send({ type: "typing", channel_id: channelId });
          }
        }}
      />
      {err && <span className="text-xs text-bad" role="alert">{err}</span>}
      <div className="flex items-center justify-between gap-2">
        <div className="relative flex items-center gap-2">
          <button
            type="button"
            aria-label="Link a task"
            title="Link a task"
            aria-expanded={taskPicker}
            onClick={() => setTaskPicker((s) => !s)}
            className="grid h-10 w-10 place-items-center rounded-lg border border-line bg-white text-ink-2 hover:bg-canvas"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M9 11l3 3 8-8 M20 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11" />
            </svg>
          </button>
          <span className="hidden text-xs text-muted sm:inline">Enter sends · Shift+Enter new line · @ mentions</span>
          {taskPicker && (
            <TaskPicker
              onPick={(key) => {
                setTaskPicker(false);
                input.current?.insert(key);
              }}
              onClose={() => setTaskPicker(false)}
            />
          )}
        </div>
        <Button type="submit" variant="primary" disabled={busy || !text.trim()}>
          Send
        </Button>
      </div>
    </form>
  );
}

function TaskPicker({ onPick, onClose }: { onPick: (key: string) => void; onClose: () => void }) {
  const [tasks, setTasks] = useState<MyTask[] | null>(null);
  const [q, setQ] = useState("");
  useEffect(() => {
    api<{ tasks: MyTask[] }>("/tasks/mine")
      .then((d) => setTasks(d.tasks.filter((t) => t.status_category !== "done")))
      .catch(() => setTasks([]));
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);
  const shown = (tasks ?? []).filter((t) => !q || `${t.key} ${t.title}`.toLowerCase().includes(q.toLowerCase())).slice(0, 8);
  return (
    <div role="dialog" aria-label="Link a task" className="absolute bottom-12 left-0 z-20 flex w-[min(360px,calc(100vw-48px))] flex-col gap-2 rounded-xl border border-line bg-white p-3 shadow-lg">
      <input autoFocus aria-label="Find a task" placeholder="Find one of your tasks" className={inputCls} value={q} onChange={(e) => setQ(e.target.value)} />
      {tasks === null && <span className="px-1 text-sm text-muted">Loading…</span>}
      {tasks && shown.length === 0 && <span className="px-1 text-sm text-muted">No open tasks assigned to you{q ? " match" : ""}. You can also type a key like FE-12.</span>}
      <ul className="flex flex-col">
        {shown.map((t) => (
          <li key={t.id}>
            <button type="button" onClick={() => onPick(t.key)} className="flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left text-sm hover:bg-canvas">
              <span className="text-xs font-bold text-muted">{t.key}</span>
              <span className="min-w-0 flex-1 truncate">{t.title}</span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function MembersPopover({ members, me, onClose }: { members: ChannelInfo["members"]; me: string; onClose: () => void }) {
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    function onDown(e: MouseEvent) {
      if (!box.current?.contains(e.target as Node)) onClose();
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    const t = setTimeout(() => document.addEventListener("mousedown", onDown));
    document.addEventListener("keydown", onKey);
    return () => {
      clearTimeout(t);
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [onClose]);
  const sorted = [...members].sort((a, b) => Number(b.online) - Number(a.online) || a.name.localeCompare(b.name));
  return (
    <div ref={box} role="dialog" aria-label="Members" className="absolute right-0 top-11 z-30 flex max-h-[60vh] w-72 flex-col overflow-y-auto rounded-xl border border-line bg-white py-2 shadow-lg">
      <span className="px-4 pb-1 text-[11px] font-semibold uppercase tracking-wider text-muted">{members.length} members</span>
      {sorted.map((m) => (
        <div key={m.id} className="flex items-center gap-2.5 px-4 py-2">
          <span className="relative">
            <Avatar id={m.id} name={m.name} size={28} square={false} />
            <OnlineDot online={m.online} className="absolute -bottom-0.5 -right-0.5" />
          </span>
          <span className="flex min-w-0 flex-1 flex-col">
            <span className="truncate text-sm font-semibold">
              {m.name}
              {m.id === me && <span className="font-normal text-muted"> (you)</span>}
            </span>
            <span className="truncate text-xs text-muted">{!m.active ? "Deactivated" : m.title}</span>
          </span>
          {m.is_manager && <span className="rounded-full bg-violet-soft px-2 py-0.5 text-[11px] font-semibold text-violet">Manager</span>}
        </div>
      ))}
    </div>
  );
}

function ChannelSettings({ info, onClose, onSaved }: { info: ChannelInfo; onClose: () => void; onSaved: (i: ChannelInfo) => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const [name, setName] = useState(info.channel.name);
  const [topic, setTopic] = useState(info.channel.topic);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    ref.current?.showModal();
  }, []);
  async function save(body: Record<string, unknown>) {
    setBusy(true);
    setErr("");
    try {
      onSaved(await api<ChannelInfo>(`/chat/channels/${info.channel.id}`, { method: "PATCH", body }));
      ref.current?.close();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <dialog ref={ref} onClose={onClose} className="m-auto w-[min(520px,calc(100vw-32px))] rounded-2xl border border-line p-0 backdrop:bg-black/40">
      <div className="flex flex-col gap-4 p-6">
        <div className="flex items-baseline justify-between">
          <h2 className="font-display text-xl font-bold">Channel settings</h2>
          <button type="button" className="text-sm font-semibold text-muted hover:text-ink" onClick={() => ref.current?.close()}>
            Close
          </button>
        </div>
        {err && <Notice tone="bad">{err}</Notice>}
        {info.can_rename && (
          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              save({ name, topic });
            }}
          >
            <label className="flex flex-col gap-1.5 text-[13px] font-semibold text-ink-2">
              Name
              <input className={inputCls} value={name} maxLength={40} onChange={(e) => setName(e.target.value)} />
              <span className="text-xs font-normal text-muted">Lower-case letters, numbers and dashes. Spaces become dashes.</span>
            </label>
            <label className="flex flex-col gap-1.5 text-[13px] font-semibold text-ink-2">
              Topic
              <input className={inputCls} value={topic} maxLength={250} onChange={(e) => setTopic(e.target.value)} placeholder="What this channel is for" />
            </label>
            <Button type="submit" variant="primary" disabled={busy} className="self-start">
              Save
            </Button>
          </form>
        )}
        {(info.can_archive || (info.channel.archived && info.can_moderate)) && (
          <div className="flex flex-col gap-2 border-t border-line pt-4">
            <h3 className="text-sm font-semibold">{info.channel.archived ? "Bring the channel back" : "Archive the channel"}</h3>
            <p className="text-[13px] text-muted">
              {info.channel.archived
                ? "Members can post in it again."
                : "Nobody can post in an archived channel. Its messages stay readable, and a manager can bring it back."}
            </p>
            <Button
              variant={info.channel.archived ? "secondary" : "danger"}
              className="self-start"
              disabled={busy}
              onClick={() => save({ archived: !info.channel.archived })}
            >
              {info.channel.archived ? "Unarchive" : "Archive channel"}
            </Button>
          </div>
        )}
      </div>
    </dialog>
  );
}
