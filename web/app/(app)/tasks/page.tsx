"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { DragEvent, FormEvent, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { addDays, dayLabel, weekdays } from "@/lib/format";
import { Board, Card as TaskCard, dueText, MyTask, priorities, priorityOf, Status, TaskTeam } from "@/lib/tasks";
import { Avatars, CheckIcon, CommentIcon } from "@/components/avatars";
import { useMe } from "@/components/shell";
import { Badge, Button, Card, cx, Empty, Field, inputCls, Notice, PageHeader } from "@/components/ui";

type View = "board" | "list" | "calendar";
const MINE = "mine";

export default function TasksPage() {
  return (
    <Suspense fallback={<p className="text-sm text-muted">Loading…</p>}>
      <TasksInner />
    </Suspense>
  );
}

function TasksInner() {
  const me = useMe();
  const params = useSearchParams();
  const router = useRouter();
  const [teams, setTeams] = useState<TaskTeam[] | null>(null);
  const [board, setBoard] = useState<Board | null>(null);
  const [mine, setMine] = useState<{ tasks: MyTask[]; today: string } | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState<{ text: string; href?: string } | null>(null);
  const [creating, setCreating] = useState(false);
  const [filters, setFilters] = useState({ q: "", person: "", priority: "", due: "", label: "" });

  const teamParam = params.get("team") ?? "";
  const view = (["board", "list", "calendar"].includes(params.get("view") ?? "") ? params.get("view") : "board") as View;

  useEffect(() => {
    api<{ teams: TaskTeam[] }>("/tasks/teams").then((r) => setTeams(r.teams)).catch((e) => setError(e.message));
  }, []);

  // The team in the URL, else my first team, else the first board I can see.
  const teamId = useMemo(() => {
    if (!teams) return "";
    if (teamParam === MINE || teams.some((t) => t.id === teamParam)) return teamParam;
    return (teams.find((t) => t.is_member) ?? teams[0])?.id ?? "";
  }, [teams, teamParam]);

  const setParam = useCallback(
    (k: string, v: string) => {
      const p = new URLSearchParams(params.toString());
      p.set(k, v);
      router.replace(`/tasks?${p.toString()}`, { scroll: false });
    },
    [params, router],
  );

  const load = useCallback(async () => {
    if (!teamId) return;
    try {
      if (teamId === MINE) {
        setBoard(null);
        setMine(await api("/tasks/mine"));
      } else {
        setMine(null);
        setBoard(await api<Board>(`/tasks/board?team_id=${teamId}`));
      }
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }, [teamId]);

  useEffect(() => {
    setBoard(null);
    load();
  }, [load]);

  const team = teams?.find((t) => t.id === teamId);
  const labels = useMemo(() => [...new Set((board?.tasks ?? []).flatMap((t) => t.labels))].sort(), [board]);

  const match = useCallback(
    (t: { title: string; key: string; labels: string[]; assignees: { id: string }[]; priority: string; due_date: string | null }, today: string, done: boolean) => {
      const f = filters;
      if (f.q && !`${t.key} ${t.title} ${t.labels.join(" ")}`.toLowerCase().includes(f.q.toLowerCase())) return false;
      if (f.person === "me" && !t.assignees.some((a) => a.id === me.id)) return false;
      if (f.person === "none" && t.assignees.length) return false;
      if (f.person && !["me", "none"].includes(f.person) && !t.assignees.some((a) => a.id === f.person)) return false;
      if (f.priority && t.priority !== f.priority) return false;
      if (f.label && !t.labels.includes(f.label)) return false;
      if (f.due === "overdue" && (done || !t.due_date || t.due_date >= today)) return false;
      if (f.due === "week" && (!t.due_date || t.due_date < today || t.due_date > addDays(today, 7))) return false;
      if (f.due === "none" && t.due_date) return false;
      return true;
    },
    [filters, me.id],
  );

  const filtering = Object.values(filters).some(Boolean);

  if (teams && teams.length === 0) {
    return (
      <>
        <PageHeader title="Tasks" />
        <Card>
          <Empty>You are not in a team yet, so there is no board to show. Ask Admin to add you to your team.</Empty>
        </Card>
      </>
    );
  }

  return (
    <>
      <PageHeader
        eyebrow={
          teamId === MINE ? (
            "Assigned to you, across your teams"
          ) : board ? (
            <span className="inline-flex items-center gap-1.5">
              <LockIcon />
              Private to the {board.team.name} team · {board.members.length} members
              {!board.is_member && " · you see it as Admin"}
            </span>
          ) : (
            " "
          )
        }
        title={teamId === MINE ? "My tasks" : team ? `${team.name} tasks` : "Tasks"}
        actions={
          <>
            <label className="flex flex-col gap-1 text-[13px] font-semibold text-ink-2">
              Board
              <select className={cx(inputCls, "w-48")} value={teamId} onChange={(e) => setParam("team", e.target.value)}>
                <option value={MINE}>My tasks (all teams)</option>
                {teams?.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name} · {t.key}
                  </option>
                ))}
              </select>
            </label>
            {teamId !== MINE && (
              <div role="group" aria-label="View" className="flex overflow-hidden rounded-[10px] border border-line-2">
                {(["board", "list", "calendar"] as View[]).map((v) => (
                  <button
                    key={v}
                    type="button"
                    aria-pressed={view === v}
                    onClick={() => setParam("view", v)}
                    className={cx("min-h-10 px-3.5 text-[13px] font-semibold capitalize", view === v ? "bg-ink text-white" : "bg-white text-ink hover:bg-canvas")}
                  >
                    {v}
                  </button>
                ))}
              </div>
            )}
            {board && (
              <Link href={`/tasks/settings?team=${board.team.id}`} className="inline-flex min-h-10 items-center rounded-[10px] border border-line-2 bg-white px-4 text-sm font-semibold hover:bg-canvas">
                Team settings
              </Link>
            )}
            {board && (
              <Button variant="primary" onClick={() => setCreating(true)}>
                + New task
              </Button>
            )}
          </>
        }
      />

      {error && <Notice tone="bad">{error}</Notice>}
      {notice && (
        <Notice tone="ok">
          {notice.text}{" "}
          {notice.href && (
            <Link className="font-semibold underline" href={notice.href}>
              Open it
            </Link>
          )}
        </Notice>
      )}

      {board && (
        <Filters
          filters={filters}
          setFilters={setFilters}
          members={board.members}
          labels={labels}
        />
      )}

      {teamId === MINE && mine && <MyTaskList data={mine} match={match} />}
      {board && view === "board" && <BoardView board={board} reload={load} match={match} filtering={filtering} onError={setError} />}
      {board && view === "list" && <ListView board={board} reload={load} match={match} onError={setError} />}
      {board && view === "calendar" && <CalendarView board={board} match={match} />}
      {!board && !mine && !error && <p className="text-sm text-muted">Loading…</p>}

      {creating && board && (
        <NewTaskDialog
          board={board}
          onClose={() => setCreating(false)}
          onCreated={(key, id) => {
            setCreating(false);
            setNotice({ text: `${key} created.`, href: `/tasks/${id}` });
            load();
          }}
        />
      )}
    </>
  );
}

function LockIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M5 11h14v10H5z M8 11V7a4 4 0 0 1 8 0v4" />
    </svg>
  );
}

type FilterState = { q: string; person: string; priority: string; due: string; label: string };

function Filters({ filters, setFilters, members, labels }: { filters: FilterState; setFilters: (f: FilterState) => void; members: Board["members"]; labels: string[] }) {
  const set = (k: keyof FilterState, v: string) => setFilters({ ...filters, [k]: v });
  const sel = "h-10 min-w-0 rounded-full border border-line-2 bg-white px-3 text-[13px] font-medium";
  return (
    <div className="flex flex-wrap items-center gap-2">
      <input type="search" aria-label="Search tasks" placeholder="Search tasks" className={cx(inputCls, "max-w-xs flex-1 basis-56")} value={filters.q} onChange={(e) => set("q", e.target.value)} />
      <select aria-label="Assignee" className={sel} value={filters.person} onChange={(e) => set("person", e.target.value)}>
        <option value="">Assignee: anyone</option>
        <option value="me">Assigned to me</option>
        <option value="none">Unassigned</option>
        {members.map((m) => (
          <option key={m.id} value={m.id}>
            {m.name}
          </option>
        ))}
      </select>
      <select aria-label="Priority" className={sel} value={filters.priority} onChange={(e) => set("priority", e.target.value)}>
        <option value="">Priority: all</option>
        {priorities.map((p) => (
          <option key={p.value} value={p.value}>
            {p.label}
          </option>
        ))}
      </select>
      <select aria-label="Due" className={sel} value={filters.due} onChange={(e) => set("due", e.target.value)}>
        <option value="">Due: any time</option>
        <option value="overdue">Overdue</option>
        <option value="week">Next 7 days</option>
        <option value="none">No due date</option>
      </select>
      <select aria-label="Label" className={sel} value={filters.label} onChange={(e) => set("label", e.target.value)}>
        <option value="">Label: all</option>
        {labels.map((l) => (
          <option key={l} value={l}>
            {l}
          </option>
        ))}
      </select>
      {Object.values(filters).some(Boolean) && (
        <Button size="sm" variant="ghost" onClick={() => setFilters({ q: "", person: "", priority: "", due: "", label: "" })}>
          Clear filters
        </Button>
      )}
    </div>
  );
}

type Matcher = (t: TaskCard | MyTask, today: string, done: boolean) => boolean;

function BoardView({ board, reload, match, filtering, onError }: { board: Board; reload: () => Promise<void>; match: Matcher; filtering: boolean; onError: (m: string) => void }) {
  const [tasks, setTasks] = useState(board.tasks);
  const [drag, setDrag] = useState<string | null>(null);
  const [over, setOver] = useState<{ status: string; index: number } | null>(null);
  useEffect(() => setTasks(board.tasks), [board.tasks]);

  const done = (s: Status) => s.category === "done";
  const column = (s: Status) => tasks.filter((t) => t.status_id === s.id);

  async function drop(s: Status) {
    const id = drag;
    const target = over;
    setDrag(null);
    setOver(null);
    if (!id || !target || target.status !== s.id) return;
    // The index counts only cards on screen; turn it into the full column's.
    const full = column(s).filter((t) => t.id !== id);
    const shown = full.filter((t) => match(t, board.today, done(s)));
    const index = target.index < shown.length ? full.indexOf(shown[target.index]) : shown.length ? full.indexOf(shown[shown.length - 1]) + 1 : full.length;
    const moving = tasks.find((t) => t.id === id)!;
    const before = tasks;
    const rest = tasks.filter((t) => t.id !== id);
    const anchor = full[index];
    const at = anchor ? rest.indexOf(anchor) : rest.length;
    setTasks([...rest.slice(0, at), { ...moving, status_id: s.id }, ...rest.slice(at)]);
    try {
      await api(`/tasks/${id}/move`, { body: { status_id: s.id, index } });
      reload();
    } catch (e) {
      setTasks(before);
      onError((e as Error).message);
    }
  }

  return (
    <div className="-mx-4 overflow-x-auto px-4 pb-2 sm:mx-0 sm:px-0">
      <div className="flex min-w-max items-start gap-3.5">
        {board.statuses.map((s) => {
          const all = column(s);
          const shown = all.filter((t) => match(t, board.today, done(s)) && t.id !== drag);
          const draggingHere = drag && over?.status === s.id;
          return (
            <section
              key={s.id}
              aria-label={s.name}
              onDragOver={(e) => {
                if (!drag) return;
                e.preventDefault();
                if (over?.status !== s.id) setOver({ status: s.id, index: shown.length });
              }}
              onDrop={(e) => {
                e.preventDefault();
                drop(s);
              }}
              className={cx("flex w-[290px] flex-none flex-col gap-2.5 rounded-[14px] p-3 transition-colors", draggingHere ? "bg-brand-soft" : "bg-[#ebedf1]")}
            >
              <div className="flex items-center gap-2 px-1 py-0.5">
                <span className="h-2.5 w-2.5 rounded-full" style={{ background: s.color }} />
                <h2 className="flex-1 text-sm font-semibold">{s.name}</h2>
                <span className="text-xs font-semibold text-muted">{filtering ? `${shown.length} of ${all.length}` : all.length}</span>
              </div>
              {shown.map((t, i) => (
                <div key={t.id}>
                  {draggingHere && over?.index === i && <DropLine />}
                  <BoardCard
                    task={t}
                    today={board.today}
                    done={done(s)}
                    onDragStart={(e) => {
                      e.dataTransfer.effectAllowed = "move";
                      e.dataTransfer.setData("text/plain", t.key);
                      setDrag(t.id);
                    }}
                    onDragEnd={() => {
                      setDrag(null);
                      setOver(null);
                    }}
                    onDragOver={(e) => {
                      if (!drag) return;
                      e.preventDefault();
                      e.stopPropagation();
                      const r = e.currentTarget.getBoundingClientRect();
                      const index = i + (e.clientY > r.top + r.height / 2 ? 1 : 0);
                      if (over?.status !== s.id || over.index !== index) setOver({ status: s.id, index });
                    }}
                  />
                </div>
              ))}
              {draggingHere && over?.index === shown.length && <DropLine />}
              {shown.length === 0 && !draggingHere && <p className="px-1 py-3 text-center text-xs text-muted">{all.length ? "No tasks match the filters" : "No tasks"}</p>}
              <QuickAdd board={board} status={s} reload={reload} onError={onError} />
            </section>
          );
        })}
      </div>
    </div>
  );
}

function DropLine() {
  return <div className="my-1 h-0.5 rounded bg-brand" aria-hidden="true" />;
}

function BoardCard({ task, today, done, ...drag }: { task: TaskCard; today: string; done: boolean; onDragStart: (e: DragEvent) => void; onDragEnd: () => void; onDragOver: (e: DragEvent<HTMLElement>) => void }) {
  const due = dueText(task.due_date, today, done, task.completed_at);
  const p = priorityOf(task.priority);
  return (
    <Link
      href={`/tasks/${task.id}`}
      draggable
      {...drag}
      className="flex cursor-grab flex-col gap-2.5 rounded-xl border border-line bg-white p-3 text-ink hover:border-line-2 active:cursor-grabbing"
    >
      <span className="flex justify-between gap-2 text-xs">
        <span className="font-semibold text-muted">{task.key}</span>
        <span className={cx("font-semibold", p.cls)}>{p.label}</span>
      </span>
      <span className={cx("text-sm font-medium leading-snug", done && "text-muted line-through decoration-1")}>{task.title}</span>
      {task.labels.length > 0 && (
        <span className="flex flex-wrap gap-1.5">
          {task.labels.map((l) => (
            <span key={l} className="rounded-md bg-canvas px-2 py-0.5 text-[11px] font-semibold text-ink-2">
              {l}
            </span>
          ))}
        </span>
      )}
      <span className="flex items-center justify-between gap-2">
        <Avatars people={task.assignees} />
        <span className="flex items-center gap-2.5 text-xs text-muted">
          {task.checklist_total > 0 && (
            <span className="inline-flex items-center gap-1" title={`${task.checklist_done} of ${task.checklist_total} subtasks done`}>
              <CheckIcon />
              {task.checklist_done}/{task.checklist_total}
            </span>
          )}
          {task.comment_count > 0 && (
            <span className="inline-flex items-center gap-1" title={`${task.comment_count} comments`}>
              <CommentIcon />
              {task.comment_count}
            </span>
          )}
          {due.text && <span className={cx("font-medium", due.late ? "text-bad" : "text-muted")}>{due.text}</span>}
        </span>
      </span>
    </Link>
  );
}

function QuickAdd({ board, status, reload, onError }: { board: Board; status: Status; reload: () => Promise<void>; onError: (m: string) => void }) {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [busy, setBusy] = useState(false);
  async function add(e: FormEvent) {
    e.preventDefault();
    if (!title.trim()) return;
    setBusy(true);
    try {
      await api("/tasks", { body: { team_id: board.team.id, title, status_id: status.id } });
      setTitle("");
      await reload();
    } catch (err) {
      onError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }
  if (!open) {
    return (
      <button type="button" onClick={() => setOpen(true)} className="min-h-10 rounded-[10px] border border-dashed border-[#b8bdc8] text-[13px] font-medium text-ink-2 hover:bg-white">
        + Add task
      </button>
    );
  }
  return (
    <form onSubmit={add} className="flex flex-col gap-2">
      <input
        autoFocus
        aria-label={`New task in ${status.name}`}
        className={inputCls}
        placeholder="Task title, then Enter"
        maxLength={200}
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        onKeyDown={(e) => e.key === "Escape" && setOpen(false)}
      />
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="ghost" onClick={() => setOpen(false)}>
          Cancel
        </Button>
        <Button size="sm" type="submit" variant="primary" disabled={busy || !title.trim()}>
          Add
        </Button>
      </div>
    </form>
  );
}

function ListView({ board, reload, match, onError }: { board: Board; reload: () => Promise<void>; match: Matcher; onError: (m: string) => void }) {
  const statusOf = (id: string) => board.statuses.find((s) => s.id === id)!;
  const rows = board.statuses.flatMap((s) => board.tasks.filter((t) => t.status_id === s.id && match(t, board.today, s.category === "done")));
  async function setStatus(id: string, status_id: string) {
    try {
      await api(`/tasks/${id}`, { method: "PATCH", body: { status_id } });
      reload();
    } catch (e) {
      onError((e as Error).message);
    }
  }
  return (
    <Card>
      {rows.length === 0 ? (
        <Empty>No tasks match.</Empty>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[860px] border-collapse text-sm">
            <thead>
              <tr className="text-left text-xs text-muted">
                {["Task", "Status", "Assignees", "Priority", "Due", "Logged", "Labels"].map((h) => (
                  <th key={h} className="px-3 py-2.5 font-semibold">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((t) => {
                const s = statusOf(t.status_id);
                const due = dueText(t.due_date, board.today, s.category === "done", t.completed_at);
                const p = priorityOf(t.priority);
                return (
                  <tr key={t.id} className="border-t border-[#eef0f3] align-middle">
                    <td className="px-3 py-2.5">
                      <Link href={`/tasks/${t.id}`} className="flex flex-col hover:underline">
                        <span className="text-xs font-semibold text-muted">{t.key}</span>
                        <span className="font-medium">{t.title}</span>
                      </Link>
                    </td>
                    <td className="px-3 py-2.5">
                      <select
                        aria-label={`Status of ${t.key}`}
                        className="h-9 rounded-lg border-2 bg-white px-2 text-[13px] font-semibold"
                        style={{ borderColor: s.color }}
                        value={t.status_id}
                        onChange={(e) => setStatus(t.id, e.target.value)}
                      >
                        {board.statuses.map((x) => (
                          <option key={x.id} value={x.id}>
                            {x.name}
                          </option>
                        ))}
                      </select>
                    </td>
                    <td className="px-3 py-2.5">{t.assignees.length ? <Avatars people={t.assignees} /> : <span className="text-muted">–</span>}</td>
                    <td className={cx("px-3 py-2.5 font-semibold", p.cls)}>{p.label}</td>
                    <td className={cx("px-3 py-2.5 whitespace-nowrap", due.late ? "font-medium text-bad" : "text-ink-2")}>{due.text || "–"}</td>
                    <td className="px-3 py-2.5 whitespace-nowrap text-ink-2">{t.logged_minutes ? hmShort(t.logged_minutes) : "–"}</td>
                    <td className="px-3 py-2.5">
                      <span className="flex flex-wrap gap-1">
                        {t.labels.map((l) => (
                          <span key={l} className="rounded-md bg-canvas px-2 py-0.5 text-[11px] font-semibold text-ink-2">
                            {l}
                          </span>
                        ))}
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

function hmShort(m: number) {
  const h = Math.floor(m / 60);
  return h ? `${h}h${m % 60 ? ` ${m % 60}m` : ""}` : `${m}m`;
}

function CalendarView({ board, match }: { board: Board; match: Matcher }) {
  const [month, setMonth] = useState(board.today.slice(0, 7));
  const [y, m] = month.split("-").map(Number);
  const first = `${month}-01`;
  const startDow = (new Date(Date.UTC(y, m - 1, 1)).getUTCDay() + 6) % 7; // Monday first
  const days = new Date(Date.UTC(y, m, 0)).getUTCDate();
  const cells: (string | null)[] = [...Array(startDow).fill(null), ...Array.from({ length: days }, (_, i) => addDays(first, i))];
  while (cells.length % 7) cells.push(null);
  const statusOf = (id: string) => board.statuses.find((s) => s.id === id)!;
  const byDay = new Map<string, TaskCard[]>();
  for (const t of board.tasks) {
    if (!t.due_date || !t.due_date.startsWith(month)) continue;
    if (!match(t, board.today, statusOf(t.status_id).category === "done")) continue;
    byDay.set(t.due_date, [...(byDay.get(t.due_date) ?? []), t]);
  }
  const noDue = board.tasks.filter((t) => !t.due_date && statusOf(t.status_id).category !== "done").length;
  const shift = (n: number) => {
    const d = new Date(Date.UTC(y, m - 1 + n, 1));
    setMonth(d.toISOString().slice(0, 7));
  };
  return (
    <Card
      title={dayLabel(first, { month: "long", year: "numeric" })}
      aside={
        <span className="flex items-center gap-2">
          {noDue > 0 && <span>{noDue} open {noDue === 1 ? "task has" : "tasks have"} no due date</span>}
          <Button size="sm" aria-label="Previous month" onClick={() => shift(-1)}>
            ←
          </Button>
          <Button size="sm" aria-label="Next month" onClick={() => shift(1)}>
            →
          </Button>
        </span>
      }
    >
      <div className="overflow-x-auto">
        <div className="grid min-w-[760px] grid-cols-7 gap-1.5">
          {weekdays.map((d) => (
            <span key={d} className="px-1 pb-1 text-xs font-semibold text-muted">
              {d}
            </span>
          ))}
          {cells.map((d, i) => (
            <div key={i} className={cx("flex min-h-24 flex-col gap-1 rounded-lg border p-1.5", d ? "border-line bg-white" : "border-transparent", d === board.today && "border-brand")}>
              {d && <span className={cx("text-xs font-semibold", d === board.today ? "text-brand" : "text-muted")}>{Number(d.slice(8))}</span>}
              {d &&
                (byDay.get(d) ?? []).map((t) => {
                  const s = statusOf(t.status_id);
                  const late = s.category !== "done" && d < board.today;
                  return (
                    <Link key={t.id} href={`/tasks/${t.id}`} title={`${t.key} ${t.title} · ${s.name}`} className={cx("truncate rounded-md px-1.5 py-1 text-[11px] font-medium", late ? "bg-bad-soft text-bad" : "bg-canvas text-ink")} style={{ borderLeft: `3px solid ${s.color}` }}>
                      <span className={cx(s.category === "done" && "line-through")}>
                        {t.key} {t.title}
                      </span>
                    </Link>
                  );
                })}
            </div>
          ))}
        </div>
      </div>
    </Card>
  );
}

function MyTaskList({ data, match }: { data: { tasks: MyTask[]; today: string }; match: Matcher }) {
  const open = data.tasks.filter((t) => t.status_category !== "done" && match(t, data.today, false));
  const done = data.tasks.filter((t) => t.status_category === "done");
  const row = (t: MyTask) => {
    const due = dueText(t.due_date, data.today, t.status_category === "done", t.completed_at);
    const p = priorityOf(t.priority);
    return (
      <Link key={t.id} href={`/tasks/${t.id}`} className="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-[#eef0f3] px-1 py-3 first:border-t-0 hover:bg-canvas">
        <span className="w-16 text-xs font-semibold text-muted">{t.key}</span>
        <span className="min-w-0 flex-1 basis-60 font-medium">{t.title}</span>
        <span className="inline-flex items-center gap-1.5 text-[13px]">
          <span className="h-2 w-2 rounded-full" style={{ background: t.status_color }} />
          {t.status_name}
        </span>
        <span className={cx("w-16 text-[13px] font-semibold", p.cls)}>{p.label}</span>
        <span className={cx("w-24 text-[13px]", due.late ? "font-medium text-bad" : "text-muted")}>{due.text || "No due date"}</span>
        <Badge>{t.team.name}</Badge>
      </Link>
    );
  };
  return (
    <>
      <Card title="Open" aside={`${open.length} tasks`}>
        {open.length ? <div className="flex flex-col">{open.map(row)}</div> : <Empty>Nothing open is assigned to you.</Empty>}
      </Card>
      {done.length > 0 && (
        <Card title="Finished in the last 14 days" aside={`${done.length} tasks`}>
          <div className="flex flex-col">{done.map(row)}</div>
        </Card>
      )}
    </>
  );
}

function NewTaskDialog({ board, onClose, onCreated }: { board: Board; onClose: () => void; onCreated: (key: string, id: string) => void }) {
  const first = board.statuses.find((s) => s.category === "not_started") ?? board.statuses[0];
  const [f, setF] = useState({ title: "", status_id: first?.id ?? "", priority: "medium", due_date: "", labels: "", description: "" });
  const [people, setPeople] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    ref.current?.showModal();
  }, []);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const r = await api<{ task: { id: string; key: string } }>("/tasks", {
        body: {
          team_id: board.team.id,
          title: f.title,
          status_id: f.status_id,
          priority: f.priority,
          due_date: f.due_date,
          labels: f.labels.split(",").map((l) => l.trim()).filter(Boolean),
          description: f.description,
          assignee_ids: people,
        },
      });
      onCreated(r.task.key, r.task.id);
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  }
  return (
    <dialog ref={ref} onClose={onClose} className="m-auto w-[min(640px,calc(100vw-32px))] rounded-2xl border border-line p-0 backdrop:bg-black/40">
      <form onSubmit={submit} className="flex flex-col gap-4 p-6">
        <div className="flex items-baseline justify-between">
          <h2 className="font-display text-xl font-bold">New task in {board.team.name}</h2>
          <button type="button" className="text-sm font-semibold text-muted hover:text-ink" onClick={() => ref.current?.close()}>
            Close
          </button>
        </div>
        {error && <Notice tone="bad">{error}</Notice>}
        <Field label="Title">
          <input autoFocus required maxLength={200} className={inputCls} value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} />
        </Field>
        <div className="grid gap-4 sm:grid-cols-3">
          <Field label="Status">
            <select className={inputCls} value={f.status_id} onChange={(e) => setF({ ...f, status_id: e.target.value })}>
              {board.statuses.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Priority">
            <select className={inputCls} value={f.priority} onChange={(e) => setF({ ...f, priority: e.target.value })}>
              {priorities.map((p) => (
                <option key={p.value} value={p.value}>
                  {p.label}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Due date">
            <input type="date" className={inputCls} value={f.due_date} min={board.today} onChange={(e) => setF({ ...f, due_date: e.target.value })} />
          </Field>
        </div>
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-1.5 text-[13px] font-semibold text-ink-2">Assignees</legend>
          <div className="flex flex-wrap gap-2">
            {board.members.map((m) => {
              const on = people.includes(m.id);
              return (
                <label key={m.id} className={cx("flex min-h-9 cursor-pointer items-center gap-2 rounded-full border px-3 text-[13px]", on ? "border-brand bg-brand-soft text-brand-ink" : "border-line-2")}>
                  <input type="checkbox" className="sr-only" checked={on} onChange={() => setPeople(on ? people.filter((x) => x !== m.id) : [...people, m.id])} />
                  {m.name}
                </label>
              );
            })}
          </div>
          <span className="text-xs text-muted">Only {board.team.name} team members can be assigned.</span>
        </fieldset>
        <Field label="Labels" hint="Separate with commas, up to 10.">
          <input className={inputCls} placeholder="Attendance, Bug" value={f.labels} onChange={(e) => setF({ ...f, labels: e.target.value })} />
        </Field>
        <Field label="Description">
          <textarea rows={4} maxLength={10000} className={cx(inputCls, "h-auto py-2")} value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} />
        </Field>
        <div className="flex justify-end gap-2">
          <Button onClick={() => ref.current?.close()}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={busy || !f.title.trim()}>
            Create task
          </Button>
        </div>
      </form>
    </dialog>
  );
}
