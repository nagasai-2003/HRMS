// Types and text helpers for chat, mentions and notifications.

export type PersonRef = { id: string; name: string };

export type TaskCard = { id: string; key: string; title: string; status_name: string; status_color: string; status_category: string };

export type Message = {
  id: string;
  channel_id: string;
  author: PersonRef;
  body: string;
  mentions: PersonRef[];
  task_refs: string[];
  tasks: TaskCard[] | null;
  edited: boolean;
  deleted: boolean;
  created_at: string;
};

export type TeamChannel = {
  id: string;
  name: string;
  topic: string;
  is_default: boolean;
  archived: boolean;
  unread: number;
  mentioned: number;
  last_message_at: string | null;
};

export type DMPerson = { id: string; name: string; title: string; teams: string; active: boolean; online: boolean };
export type DM = { id: string; members: DMPerson[]; unread: number; last_message_at: string | null };

export type Conversations = {
  teams: { id: string; name: string; key: string; is_manager: boolean; channels: TeamChannel[] }[];
  dms: DM[];
  unread: number;
};

export type ChannelMember = { id: string; name: string; title: string; active: boolean; is_manager: boolean; online: boolean };

export type ChannelInfo = {
  channel: {
    id: string;
    kind: "team" | "dm";
    name: string;
    topic: string;
    team: { id: string; name: string; key: string } | null;
    is_default: boolean;
    archived: boolean;
    created_at: string;
  };
  members: ChannelMember[];
  last_read_id: string;
  can_post: boolean;
  can_rename: boolean;
  can_archive: boolean;
  can_moderate: boolean;
};

export type Notification = {
  id: string;
  kind: "task_assigned" | "task_mention" | "task_comment" | "chat_mention";
  title: string;
  body: string;
  link: string;
  actor_name: string;
  created_at: string;
  read: boolean;
};

export function dmTitle(members: { name: string }[]): string {
  if (members.length === 0) return "Just you";
  return members.map((m) => m.name).join(", ");
}

/** Message ids are big integers sent as strings. */
export function idNum(id: string): number {
  return Number(id);
}

// ---- mentions ----
//
// In the composer a mention is plain "@Ananya Rao"; the server stores
// <@user-id>. toBody / fromBody convert, longest names first so "@Ana Rao"
// never eats part of "@Ana Rao Kumar".

export function toBody(text: string, picked: PersonRef[]): string {
  const sorted = [...new Map(picked.map((p) => [p.id, p])).values()].sort((a, b) => b.name.length - a.name.length);
  let out = text;
  for (const p of sorted) {
    out = out.split("@" + p.name).join(`<@${p.id}>`);
  }
  return out;
}

export function fromBody(body: string, mentions: PersonRef[]): { text: string; picked: PersonRef[] } {
  const names = new Map(mentions.map((m) => [m.id, m.name]));
  const picked: PersonRef[] = [];
  const text = body.replace(/<@([0-9a-fA-F-]{36})>/g, (_, id: string) => {
    const name = names.get(id);
    if (!name) return "@someone";
    picked.push({ id, name });
    return "@" + name;
  });
  return { text, picked };
}

export type Piece =
  | { kind: "text"; text: string }
  | { kind: "mention"; id: string; name: string }
  | { kind: "link"; href: string; text: string }
  | { kind: "task"; key: string };

const tokenRe = /<@([0-9a-fA-F-]{36})>|(https?:\/\/[^\s<>"]+[^\s<>".,;:!?)\]])|\b([A-Z][A-Z0-9]{1,5}-[1-9][0-9]{0,6})\b/g;

/** Splits stored text into plain text, mentions, links and task keys. */
export function pieces(body: string, mentions: PersonRef[]): Piece[] {
  const names = new Map(mentions.map((m) => [m.id, m.name]));
  const out: Piece[] = [];
  let last = 0;
  for (const m of body.matchAll(tokenRe)) {
    const at = m.index ?? 0;
    if (at > last) out.push({ kind: "text", text: body.slice(last, at) });
    if (m[1]) out.push({ kind: "mention", id: m[1], name: names.get(m[1]) ?? "someone" });
    else if (m[2]) out.push({ kind: "link", href: m[2], text: m[2] });
    else if (m[3]) out.push({ kind: "task", key: m[3] });
    last = at + m[0].length;
  }
  if (last < body.length) out.push({ kind: "text", text: body.slice(last) });
  return out;
}

const tz = "Asia/Kolkata";

export function dayKey(iso: string): string {
  return new Date(iso).toLocaleDateString("en-CA", { timeZone: tz });
}

export function dayDivider(iso: string, now = new Date()): string {
  const k = dayKey(iso);
  const today = dayKey(now.toISOString());
  const yesterday = dayKey(new Date(now.getTime() - 86400000).toISOString());
  if (k === today) return "Today";
  if (k === yesterday) return "Yesterday";
  return new Date(iso).toLocaleDateString("en-GB", { weekday: "long", day: "numeric", month: "short", timeZone: tz });
}

export function timeOf(iso: string): string {
  return new Date(iso).toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", timeZone: tz });
}

export function ago(iso: string, now = Date.now()): string {
  const s = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "short", timeZone: tz });
}
