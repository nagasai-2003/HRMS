"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { FormEvent, useCallback, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { hm } from "@/lib/format";
import { avatarColor, categoryLabel, dueText, initials, priorities, priorityOf, TaskDetail, when } from "@/lib/tasks";
import { Button, Card, cx, Empty, inputCls, Notice } from "@/components/ui";
import { MentionInput } from "@/components/mention-input";
import { RichText } from "@/components/rich-text";
import { useMe } from "@/components/shell";
import { PersonRef, toBody } from "@/lib/chat";

export default function TaskPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const me = useMe();
  const [d, setD] = useState<TaskDetail | null>(null);
  const [error, setError] = useState("");
  const [missing, setMissing] = useState(false);

  const load = useCallback(() => {
    api<TaskDetail>(`/tasks/${id}`)
      .then((r) => {
        setD(r);
        setError("");
      })
      .catch((e) => (e instanceof ApiError && e.status === 404 ? setMissing(true) : setError(e.message)));
  }, [id]);
  useEffect(load, [load]);

  // Every change answers with the whole task, so the page simply swaps it in.
  async function change(path: string, method: string, body?: unknown) {
    try {
      setD(await api<TaskDetail>(path, { method, body }));
      setError("");
      return true;
    } catch (e) {
      setError((e as Error).message);
      return false;
    }
  }
  const patch = (body: Record<string, unknown>) => change(`/tasks/${id}`, "PATCH", body);

  if (missing) {
    return (
      <Card>
        <Empty>
          This task does not exist, or it belongs to a team you are not in.{" "}
          <Link className="font-semibold text-brand" href="/tasks">
            Back to tasks
          </Link>
        </Empty>
      </Card>
    );
  }
  if (!d) return <p className="text-sm text-muted">{error || "Loading…"}</p>;
  const t = d.task;
  const done = t.status_category === "done";
  const due = dueText(t.due_date, d.today, done, t.completed_at);
  const checked = d.checklist.filter((c) => c.done).length;

  return (
    <>
      <nav aria-label="Breadcrumb" className="flex flex-wrap gap-2 text-[13px] text-muted">
        <Link className="text-brand hover:underline" href={`/tasks?team=${t.team.id}`}>
          {t.team.name} tasks
        </Link>
        <span>/</span>
        <span className="font-semibold text-ink">{t.key}</span>
      </nav>
      {error && <Notice tone="bad">{error}</Notice>}

      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_320px]">
        <article className="flex min-w-0 flex-col gap-6 rounded-2xl border border-line bg-white p-6">
          <Title value={t.title} onSave={(title) => patch({ title })} />
          <Description value={t.description} onSave={(description) => patch({ description })} />

          <section className="flex flex-col gap-2.5">
            <div className="flex items-baseline justify-between">
              <h2 className="text-sm font-semibold">
                Subtasks{d.checklist.length > 0 && ` · ${checked} of ${d.checklist.length} done`}
              </h2>
            </div>
            {d.checklist.length > 0 && (
              <div className="h-1.5 rounded bg-[#eef0f3]" aria-hidden="true">
                <div className="h-1.5 rounded bg-[#15803d]" style={{ width: `${(checked / d.checklist.length) * 100}%` }} />
              </div>
            )}
            {d.checklist.map((c) => (
              <div key={c.id} className="group flex min-h-10 items-center gap-2.5 border-b border-[#eef0f3] text-sm">
                <input
                  id={`c-${c.id}`}
                  type="checkbox"
                  className="h-[18px] w-[18px] accent-[#15803d]"
                  checked={c.done}
                  onChange={async (e) => {
                    // Tick at once; the server's answer replaces it, or a failure reloads.
                    const done = e.target.checked;
                    setD({ ...d, checklist: d.checklist.map((x) => (x.id === c.id ? { ...x, done } : x)) });
                    if (!(await change(`/tasks/${id}/checklist/${c.id}`, "PATCH", { done }))) load();
                  }}
                />
                <label htmlFor={`c-${c.id}`} className={cx("flex-1", c.done && "text-muted line-through")}>
                  {c.title}
                </label>
                <button
                  type="button"
                  aria-label={`Delete subtask ${c.title}`}
                  className="rounded px-2 text-xs font-semibold text-muted opacity-0 hover:text-bad focus:opacity-100 group-hover:opacity-100"
                  onClick={() => change(`/tasks/${id}/checklist/${c.id}`, "DELETE")}
                >
                  Delete
                </button>
              </div>
            ))}
            <AddLine placeholder="Add a subtask" label="Add subtask" onAdd={(title) => change(`/tasks/${id}/checklist`, "POST", { title })} />
          </section>

          <section className="flex flex-col gap-4 border-t border-[#eef0f3] pt-5">
            <h2 className="text-sm font-semibold">Comments</h2>
            {d.comments.length === 0 && <p className="text-sm text-muted">No comments yet.</p>}
            {d.comments.map((c) => (
              <div key={c.id} className="flex gap-3">
                <span className="grid h-8 w-8 flex-none place-items-center rounded-full text-xs font-bold text-white" style={{ background: avatarColor(c.author_id) }}>
                  {initials(c.author_name)}
                </span>
                <div className="flex min-w-0 flex-1 flex-col gap-1">
                  <span className="text-[13px]">
                    <strong>{c.author_name}</strong> <span className="text-muted">{when(c.created_at)}</span>
                    {c.can_delete && (
                      <button type="button" className="ml-3 text-xs font-semibold text-muted hover:text-bad" onClick={() => confirm("Delete this comment?") && change(`/tasks/${id}/comments/${c.id}`, "DELETE")}>
                        Delete
                      </button>
                    )}
                  </span>
                  <p className="text-sm leading-relaxed text-ink-2">
                    <RichText body={c.body} mentions={c.mentions} me={me.id} />
                  </p>
                </div>
              </div>
            ))}
            <CommentBox people={d.members.filter((m) => m.id !== me.id)} onSend={(body) => change(`/tasks/${id}/comments`, "POST", { body })} />
          </section>
        </article>

        <aside aria-label="Task fields" className="flex min-w-0 flex-col gap-4">
          <section className="flex flex-col gap-4 rounded-2xl border border-line bg-white p-5">
            <label className="flex flex-col gap-1.5 text-xs font-semibold text-muted">
              Status
              <select
                className="h-11 rounded-[10px] border-2 bg-white px-3 text-sm font-semibold text-ink"
                style={{ borderColor: t.status_color }}
                value={t.status_id}
                onChange={(e) => patch({ status_id: e.target.value })}
              >
                {d.statuses.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name} ({categoryLabel[s.category]})
                  </option>
                ))}
              </select>
            </label>
            <Assignees d={d} onSave={(assignee_ids) => patch({ assignee_ids })} />
            <Row label="Priority">
              <select aria-label="Priority" className={cx("h-9 rounded-lg border border-line-2 bg-white px-2 text-sm font-semibold", priorityOf(t.priority).cls)} value={t.priority} onChange={(e) => patch({ priority: e.target.value })}>
                {priorities.map((p) => (
                  <option key={p.value} value={p.value}>
                    {p.label}
                  </option>
                ))}
              </select>
            </Row>
            <Row label="Due date" note={due.text && !done ? <span className={due.late ? "text-bad" : "text-muted"}>{due.text}</span> : undefined}>
              <input aria-label="Due date" type="date" className="h-9 rounded-lg border border-line-2 bg-white px-2 text-sm" value={t.due_date ?? ""} onChange={(e) => patch({ due_date: e.target.value })} />
            </Row>
            <Row label="Start date">
              <input aria-label="Start date" type="date" className="h-9 rounded-lg border border-line-2 bg-white px-2 text-sm" value={t.start_date ?? ""} onChange={(e) => patch({ start_date: e.target.value })} />
            </Row>
            <Estimate minutes={t.estimate_minutes} logged={d.time.logged_minutes} onSave={(estimate_minutes) => patch({ estimate_minutes })} />
            <Labels value={t.labels} onSave={(labels) => patch({ labels })} />
            <Row label="Reporter">
              <span className="text-sm font-medium">{t.reporter.name}</span>
            </Row>
            {done && t.completed_at && (
              <Row label="Completed">
                <span className="text-sm font-medium text-ok">{when(t.completed_at)}</span>
              </Row>
            )}
          </section>

          <section className="flex flex-col gap-2 rounded-2xl border border-line bg-white p-5">
            <h2 className="text-sm font-semibold">Time from timesheets</h2>
            {d.time.by_person.length === 0 ? (
              <p className="text-[13px] leading-relaxed text-muted">
                No hours yet. Type <strong className="text-ink">{t.key}</strong> in a timesheet row&apos;s Task box to log time here.
              </p>
            ) : (
              <>
                {d.time.by_person.map((p) => (
                  <div key={p.id} className="flex justify-between text-sm">
                    <span>{p.name}</span>
                    <span className="font-medium">{hm(p.minutes)}</span>
                  </div>
                ))}
                <div className="flex justify-between border-t border-[#eef0f3] pt-2 text-sm font-semibold">
                  <span>Total</span>
                  <span>{hm(d.time.logged_minutes)}</span>
                </div>
              </>
            )}
          </section>

          <section className="flex flex-col gap-1 rounded-2xl border border-line bg-white p-5">
            <h2 className="mb-1 text-sm font-semibold">Activity</h2>
            {d.activity.map((a) => (
              <div key={a.id} className="flex flex-col gap-0.5 border-b border-[#eef0f3] py-1.5 text-[13px] last:border-0">
                <span>
                  <strong className="font-semibold">{a.actor_name}</strong> {a.text}
                </span>
                <span className="text-xs text-muted">{when(a.created_at)}</span>
              </div>
            ))}
          </section>

          {d.can_delete && (
            <Button
              variant="danger"
              onClick={async () => {
                if (!confirm(`Delete ${t.key}? Hours logged against it stay on the timesheets.`)) return;
                try {
                  await api(`/tasks/${id}`, { method: "DELETE" });
                  router.replace(`/tasks?team=${t.team.id}`);
                } catch (e) {
                  setError((e as Error).message);
                }
              }}
            >
              Delete task
            </Button>
          )}
        </aside>
      </div>
    </>
  );
}

function Row({ label, children, note }: { label: string; children: React.ReactNode; note?: React.ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-t border-[#eef0f3] pt-3 text-sm">
      <span className="text-muted">{label}</span>
      <span className="flex items-center gap-2">
        {note && <span className="text-xs font-medium">{note}</span>}
        {children}
      </span>
    </div>
  );
}

function Title({ value, onSave }: { value: string; onSave: (v: string) => Promise<boolean> }) {
  const [v, setV] = useState(value);
  useEffect(() => setV(value), [value]);
  const save = async () => {
    if (v.trim() && v.trim() !== value && !(await onSave(v))) setV(value);
    if (!v.trim()) setV(value);
  };
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor="task-title" className="text-xs font-semibold text-muted">
        Title
      </label>
      <input
        id="task-title"
        maxLength={200}
        className="-ml-1.5 rounded-lg border border-transparent px-1.5 py-1 font-display text-2xl font-bold hover:border-line focus:border-brand focus:outline-none"
        value={v}
        onChange={(e) => setV(e.target.value)}
        onBlur={save}
        onKeyDown={(e) => {
          if (e.key === "Enter") (e.target as HTMLInputElement).blur();
          if (e.key === "Escape") setV(value);
        }}
      />
    </div>
  );
}

function Description({ value, onSave }: { value: string; onSave: (v: string) => Promise<boolean> }) {
  const [editing, setEditing] = useState(false);
  const [v, setV] = useState(value);
  useEffect(() => setV(value), [value]);
  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-baseline justify-between">
        <h2 className="text-sm font-semibold">Description</h2>
        {!editing && (
          <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
            {value ? "Edit" : "Add"}
          </Button>
        )}
      </div>
      {editing ? (
        <>
          <textarea autoFocus rows={6} maxLength={10000} className={cx(inputCls, "h-auto py-2 leading-relaxed")} value={v} onChange={(e) => setV(e.target.value)} />
          <div className="flex justify-end gap-2">
            <Button
              size="sm"
              onClick={() => {
                setV(value);
                setEditing(false);
              }}
            >
              Cancel
            </Button>
            <Button size="sm" variant="primary" onClick={async () => (await onSave(v)) && setEditing(false)}>
              Save
            </Button>
          </div>
        </>
      ) : value ? (
        <p className="whitespace-pre-wrap break-words text-sm leading-relaxed text-ink-2">{value}</p>
      ) : (
        <p className="text-sm text-muted">No description.</p>
      )}
    </section>
  );
}

function AddLine({ placeholder, label, onAdd }: { placeholder: string; label: string; onAdd: (v: string) => Promise<boolean> }) {
  const [v, setV] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <form
      className="flex gap-2"
      onSubmit={async (e: FormEvent) => {
        e.preventDefault();
        if (!v.trim()) return;
        setBusy(true);
        if (await onAdd(v)) setV("");
        setBusy(false);
      }}
    >
      <input aria-label={label} placeholder={placeholder} maxLength={200} className={cx(inputCls, "flex-1")} value={v} onChange={(e) => setV(e.target.value)} />
      <Button type="submit" disabled={busy || !v.trim()}>
        {label}
      </Button>
    </form>
  );
}

function CommentBox({ people, onSend }: { people: { id: string; name: string; title: string }[]; onSend: (v: string) => Promise<boolean> }) {
  const [v, setV] = useState("");
  const [picked, setPicked] = useState<PersonRef[]>([]);
  const [busy, setBusy] = useState(false);
  return (
    <form
      className="flex flex-col gap-2"
      onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        if (await onSend(toBody(v, picked))) {
          setV("");
          setPicked([]);
        }
        setBusy(false);
      }}
    >
      <div className="flex flex-col gap-1.5 text-[13px] font-semibold text-ink-2">
        <span>Add a comment</span>
        <MentionInput
          label="Add a comment"
          minRows={3}
          placeholder="Write a comment. Type @ to mention a team member."
          value={v}
          onChange={setV}
          picked={picked}
          onPick={setPicked}
          people={people}
          className="font-normal"
        />
      </div>
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-muted">People you mention get a notification. Assignees and the reporter hear about every comment.</span>
        <Button type="submit" variant="primary" disabled={busy || !v.trim()}>
          Comment
        </Button>
      </div>
    </form>
  );
}

function Assignees({ d, onSave }: { d: TaskDetail; onSave: (ids: string[]) => Promise<boolean> }) {
  const ids = d.task.assignees.map((a) => a.id);
  const others = d.members.filter((m) => !ids.includes(m.id));
  return (
    <div className="flex flex-col gap-2">
      <span className="text-xs font-semibold text-muted">Assignees</span>
      <div className="flex flex-wrap gap-2">
        {d.task.assignees.map((a) => (
          <span key={a.id} className="flex items-center gap-1.5 rounded-full bg-canvas py-1 pl-1 pr-1.5 text-[13px]">
            <span className="grid h-6 w-6 place-items-center rounded-full text-[10px] font-bold text-white" style={{ background: avatarColor(a.id) }}>
              {initials(a.name)}
            </span>
            {a.name}
            <button type="button" aria-label={`Unassign ${a.name}`} className="grid h-6 w-6 place-items-center rounded-full text-muted hover:bg-white hover:text-bad" onClick={() => onSave(ids.filter((x) => x !== a.id))}>
              ×
            </button>
          </span>
        ))}
        {d.task.assignees.length === 0 && <span className="text-sm text-muted">No one yet</span>}
      </div>
      {others.length > 0 && (
        <select aria-label="Add assignee" className="h-9 rounded-lg border border-dashed border-line-2 bg-white px-2 text-[13px]" value="" onChange={(e) => e.target.value && onSave([...ids, e.target.value])}>
          <option value="">+ Assign someone…</option>
          {others.map((m) => (
            <option key={m.id} value={m.id}>
              {m.name}
            </option>
          ))}
        </select>
      )}
      <span className="text-xs text-muted">Only {d.task.team.name} team members can be assigned.</span>
    </div>
  );
}

function Estimate({ minutes, logged, onSave }: { minutes: number | null; logged: number; onSave: (m: number) => Promise<boolean> }) {
  const shown = minutes ? String(+(minutes / 60).toFixed(2)) : "";
  const [v, setV] = useState(shown);
  useEffect(() => setV(shown), [shown]);
  const over = minutes != null && logged > minutes;
  return (
    <Row label="Estimate (hours)" note={minutes ? <span className={over ? "text-bad" : "text-muted"}>logged {hm(logged)}</span> : undefined}>
      <input
        aria-label="Estimate in hours"
        inputMode="decimal"
        className="h-9 w-20 rounded-lg border border-line-2 bg-white px-2 text-sm"
        placeholder="–"
        value={v}
        onChange={(e) => setV(e.target.value)}
        onBlur={async () => {
          if (v === shown) return;
          const n = v.trim() === "" ? 0 : Math.round(Number(v.replace(",", ".")) * 60);
          if (!Number.isFinite(n) || n < 0 || !(await onSave(n))) setV(shown);
        }}
        onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
      />
    </Row>
  );
}

function Labels({ value, onSave }: { value: string[]; onSave: (l: string[]) => Promise<boolean> }) {
  const shown = value.join(", ");
  const [v, setV] = useState(shown);
  useEffect(() => setV(shown), [shown]);
  return (
    <div className="flex flex-col gap-1.5 border-t border-[#eef0f3] pt-3 text-sm">
      <label htmlFor="task-labels" className="text-muted">
        Labels
      </label>
      <input
        id="task-labels"
        className={inputCls}
        placeholder="Attendance, Bug"
        value={v}
        onChange={(e) => setV(e.target.value)}
        onBlur={async () => {
          if (v === shown) return;
          if (!(await onSave(v.split(",").map((x) => x.trim()).filter(Boolean)))) setV(shown);
        }}
        onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
      />
    </div>
  );
}
