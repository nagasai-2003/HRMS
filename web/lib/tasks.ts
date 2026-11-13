// Types and small helpers shared by the task pages.

import { dayLabel } from "./format";

export type Category = "not_started" | "active" | "done";
export type Priority = "low" | "medium" | "high" | "urgent";

export type Status = { id: string; name: string; color: string; category: Category; position: number; task_count: number };
export type Person = { id: string; name: string };
export type Member = Person & { title: string; is_manager: boolean };

export type Card = {
  id: string;
  key: string;
  number: number;
  title: string;
  status_id: string;
  priority: Priority;
  labels: string[];
  start_date: string | null;
  due_date: string | null;
  estimate_minutes: number | null;
  logged_minutes: number;
  completed_at: string | null;
  checklist_total: number;
  checklist_done: number;
  comment_count: number;
  assignees: Person[];
};

export type Board = {
  team: { id: string; name: string; key: string };
  can_manage: boolean;
  is_member: boolean;
  statuses: Status[];
  tasks: Card[];
  members: Member[];
  today: string;
};

export type TaskTeam = { id: string; name: string; key: string; is_manager: boolean; is_member: boolean; open_tasks: number };

export type MyTask = Omit<Card, "number" | "checklist_total" | "checklist_done" | "comment_count"> & {
  team: { id: string; name: string; key: string };
  status_name: string;
  status_color: string;
  status_category: Category;
};

export type TaskDetail = {
  task: {
    id: string;
    key: string;
    number: number;
    title: string;
    description: string;
    status_id: string;
    status_name: string;
    status_color: string;
    status_category: Category;
    priority: Priority;
    labels: string[];
    start_date: string | null;
    due_date: string | null;
    estimate_minutes: number | null;
    completed_at: string | null;
    created_at: string;
    updated_at: string;
    reporter: Person;
    team: { id: string; name: string; key: string };
    assignees: Person[];
  };
  statuses: Status[];
  members: Member[];
  checklist: { id: string; title: string; done: boolean }[];
  comments: { id: string; author_id: string; author_name: string; body: string; created_at: string; can_delete: boolean }[];
  activity: { id: string; actor_name: string; text: string; created_at: string }[];
  time: { logged_minutes: number; by_person: { id: string; name: string; minutes: number }[] };
  can_delete: boolean;
  can_manage: boolean;
  today: string;
};

export const priorities: { value: Priority; label: string; cls: string }[] = [
  { value: "urgent", label: "Urgent", cls: "text-bad" },
  { value: "high", label: "High", cls: "text-bad" },
  { value: "medium", label: "Medium", cls: "text-warn" },
  { value: "low", label: "Low", cls: "text-muted" },
];

export const priorityOf = (p: Priority) => priorities.find((x) => x.value === p)!;

export const categoryLabel: Record<Category, string> = { not_started: "Not started", active: "Active", done: "Done" };

export const swatches = [
  { label: "Grey", color: "#8A909C" },
  { label: "Blue", color: "#2F5BD3" },
  { label: "Teal", color: "#0E7490" },
  { label: "Purple", color: "#7C3AED" },
  { label: "Orange", color: "#D97706" },
  { label: "Red", color: "#B42318" },
  { label: "Green", color: "#15803D" },
];

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/);
  return ((parts[0]?.[0] ?? "") + (parts.length > 1 ? parts[parts.length - 1][0] : "")).toUpperCase();
}

const avatarColors = ["#2F5BD3", "#B45309", "#15803D", "#7C3AED", "#0E7490", "#9B1C14", "#3A3F4A"];
export function avatarColor(id: string): string {
  let h = 0;
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return avatarColors[h % avatarColors.length];
}

/** "Today", "Overdue 2d", "18 Nov" or "Done 11 Nov", with whether it is late. */
export function dueText(due: string | null, today: string, done: boolean, completedAt?: string | null): { text: string; late: boolean } {
  if (done) {
    if (completedAt) {
      const d = new Date(completedAt).toLocaleDateString("en-GB", { day: "numeric", month: "short", timeZone: "Asia/Kolkata" });
      return { text: `Done ${d}`, late: false };
    }
    return { text: "Done", late: false };
  }
  if (!due) return { text: "", late: false };
  if (due === today) return { text: "Due today", late: true };
  if (due < today) {
    const days = Math.round((Date.parse(today) - Date.parse(due)) / 86400000);
    return { text: `Overdue ${days}d`, late: true };
  }
  return { text: dayLabel(due, { day: "numeric", month: "short" }), late: false };
}

export function when(iso: string): string {
  return new Date(iso).toLocaleString("en-GB", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", timeZone: "Asia/Kolkata" });
}
