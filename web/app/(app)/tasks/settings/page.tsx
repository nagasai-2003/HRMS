"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { FormEvent, Suspense, useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { Board, Category, categoryLabel, Status, swatches } from "@/lib/tasks";
import { useMe } from "@/components/shell";
import { Badge, Button, Card, cx, Empty, Field, inputCls, Notice, PageHeader, Tabs } from "@/components/ui";

const templates = [
  { id: "default", name: "Standard", flow: "To do · In progress · In review · Blocked · Done" },
  { id: "software", name: "Software", flow: "To do · In progress · In review · QA · Done" },
  { id: "design", name: "Design", flow: "Brief · Exploring · Feedback · Final · Delivered" },
  { id: "sales", name: "Sales", flow: "Lead · Contacted · Demo · Negotiation · Won · Lost" },
];

const catTone: Record<Category, "neutral" | "info" | "ok"> = { not_started: "neutral", active: "info", done: "ok" };

export default function SettingsPage() {
  return (
    <Suspense fallback={<p className="text-sm text-muted">Loading…</p>}>
      <Inner />
    </Suspense>
  );
}

function Inner() {
  const teamId = useSearchParams().get("team") ?? "";
  const me = useMe();
  const [board, setBoard] = useState<Board | null>(null);
  const [tab, setTab] = useState<"groups" | "members">("groups");
  const [error, setError] = useState("");
  const [ok, setOk] = useState("");

  const load = useCallback(() => {
    api<Board>(`/tasks/board?team_id=${teamId}`).then(setBoard).catch((e) => setError(e.message));
  }, [teamId]);
  useEffect(load, [load]);

  async function run(fn: () => Promise<{ statuses: Status[] }>, done?: string) {
    setError("");
    setOk("");
    try {
      const r = await fn();
      setBoard((b) => (b ? { ...b, statuses: r.statuses } : b));
      if (done) setOk(done);
      return true;
    } catch (e) {
      setError((e as Error).message);
      return false;
    }
  }

  if (!board) return <p className="text-sm text-muted">{error || "Loading…"}</p>;
  const base = `/tasks/teams/${board.team.id}/statuses`;
  const canEdit = board.can_manage;
  const totalTasks = board.statuses.reduce((n, s) => n + s.task_count, 0);

  return (
    <>
      <div className="flex flex-col gap-1">
        <Link href={`/tasks?team=${board.team.id}`} className="text-[13px] font-semibold text-brand hover:underline">
          Back to {board.team.name} tasks
        </Link>
        <PageHeader title={`${board.team.name} team settings`} eyebrow={`Task numbers start with ${board.team.key}. ${canEdit ? (me.role === "admin" ? "You can edit as Admin." : "You manage this team, so you can edit.") : "Team managers and Admin can edit. Members can view."}`} />
      </div>
      <Tabs value={tab} onChange={setTab} items={[{ value: "groups", label: "Status groups" }, { value: "members", label: `Members (${board.members.length})` }]} />
      {error && <Notice tone="bad">{error}</Notice>}
      {ok && <Notice tone="ok">{ok}</Notice>}

      {tab === "members" ? (
        <Card title="Members" aside="Admin adds and removes people on Teams and people">
          {board.members.length === 0 ? (
            <Empty>No one is in this team.</Empty>
          ) : (
            <ul className="flex flex-col">
              {board.members.map((m) => (
                <li key={m.id} className="flex items-center justify-between gap-3 border-t border-[#eef0f3] py-3 first:border-0">
                  <span className="flex flex-col">
                    <span className="font-semibold">{m.name}</span>
                    <span className="text-xs text-muted">{m.title}</span>
                  </span>
                  {m.is_manager && <Badge tone="info">Team manager</Badge>}
                </li>
              ))}
            </ul>
          )}
        </Card>
      ) : (
        <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_300px]">
          <Card title="Status groups" aside="The order here is the column order on the board">
            <ul className="flex flex-col">
              {board.statuses.map((s, i) => (
                <GroupRow
                  key={s.id}
                  s={s}
                  first={i === 0}
                  last={i === board.statuses.length - 1}
                  canEdit={canEdit}
                  others={board.statuses.filter((x) => x.id !== s.id)}
                  onMove={(dir) => {
                    const ids = board.statuses.map((x) => x.id);
                    [ids[i], ids[i + dir]] = [ids[i + dir], ids[i]];
                    return run(() => api(`${base}/order`, { method: "PUT", body: { ids } }));
                  }}
                  onSave={(body) => run(() => api(`${base}/${s.id}`, { method: "PATCH", body }), `"${body.name}" saved.`)}
                  onDelete={(moveTo) =>
                    run(
                      () => api(`${base}/${s.id}${moveTo ? `?move_to=${moveTo}` : ""}`, { method: "DELETE" }),
                      `"${s.name}" deleted${moveTo ? `; its tasks moved to "${board.statuses.find((x) => x.id === moveTo)?.name}"` : ""}.`,
                    ).then((r) => (r ? load() : undefined))
                  }
                />
              ))}
            </ul>
            {canEdit && <AddGroup onAdd={(body) => run(() => api(base, { body }), `"${body.name}" added.`)} />}
          </Card>

          <div className="flex flex-col gap-4">
            <Card title={'What "Counts as" does'}>
              <p className="text-[13px] leading-relaxed text-ink-2">
                Every team names its own groups. Each one counts as <strong>Not started</strong>, <strong>Active</strong> or <strong>Done</strong>, so dashboards can compare teams.
                Moving a task into a Done group marks it completed, with the time it happened. A board always keeps at least one Done group.
              </p>
            </Card>
            {canEdit && (
              <Card title="Start from a template">
                <div className="flex flex-col gap-2">
                  {templates.map((t) => (
                    <button
                      key={t.id}
                      type="button"
                      disabled={totalTasks > 0}
                      onClick={() => confirm(`Replace every group with the ${t.name} template?`) && run(() => api(`${base}/template`, { body: { template: t.id } }), `${t.name} template applied.`)}
                      className="flex flex-col gap-0.5 rounded-[10px] border border-line px-3 py-2.5 text-left hover:bg-canvas disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      <span className="text-sm font-semibold">{t.name}</span>
                      <span className="text-xs text-muted">{t.flow}</span>
                    </button>
                  ))}
                </div>
                {totalTasks > 0 && <p className="mt-3 text-xs leading-relaxed text-muted">Templates replace every group, so they work only on a board with no tasks. Edit the groups one by one instead.</p>}
              </Card>
            )}
          </div>
        </div>
      )}
    </>
  );
}

type GroupBody = { name: string; color: string; category: Category };

function GroupFields({ v, set }: { v: GroupBody; set: (v: GroupBody) => void }) {
  return (
    <div className="flex flex-wrap items-end gap-3">
      <Field label="Name">
        <input required maxLength={40} className={cx(inputCls, "w-48")} value={v.name} onChange={(e) => set({ ...v, name: e.target.value })} />
      </Field>
      <fieldset className="flex flex-col gap-1.5">
        <legend className="mb-1.5 text-[13px] font-semibold text-ink-2">Colour</legend>
        <div className="flex gap-1.5">
          {swatches.map((s) => (
            <button
              key={s.color}
              type="button"
              aria-label={s.label}
              aria-pressed={v.color.toUpperCase() === s.color}
              onClick={() => set({ ...v, color: s.color })}
              className={cx("h-8 w-8 rounded-full border-[3px]", v.color.toUpperCase() === s.color ? "border-ink" : "border-white")}
              style={{ background: s.color }}
            />
          ))}
        </div>
      </fieldset>
      <Field label="Counts as">
        <select className={cx(inputCls, "w-44")} value={v.category} onChange={(e) => set({ ...v, category: e.target.value as Category })}>
          <option value="not_started">Not started</option>
          <option value="active">Active (in progress)</option>
          <option value="done">Done (completed)</option>
        </select>
      </Field>
    </div>
  );
}

function GroupRow({
  s,
  first,
  last,
  canEdit,
  others,
  onMove,
  onSave,
  onDelete,
}: {
  s: Status;
  first: boolean;
  last: boolean;
  canEdit: boolean;
  others: Status[];
  onMove: (dir: -1 | 1) => Promise<boolean>;
  onSave: (b: GroupBody) => Promise<boolean>;
  onDelete: (moveTo: string) => Promise<void>;
}) {
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [v, setV] = useState<GroupBody>({ name: s.name, color: s.color, category: s.category });
  const [moveTo, setMoveTo] = useState(others[0]?.id ?? "");
  useEffect(() => setV({ name: s.name, color: s.color, category: s.category }), [s]);

  return (
    <li className="flex flex-col gap-3 border-t border-[#eef0f3] py-3 first:border-0">
      <div className="flex flex-wrap items-center gap-3">
        {canEdit && (
          <span className="flex flex-col">
            <button type="button" aria-label={`Move ${s.name} left on the board`} disabled={first} onClick={() => onMove(-1)} className="h-5 w-7 rounded text-xs text-muted hover:bg-canvas disabled:opacity-30">
              ▲
            </button>
            <button type="button" aria-label={`Move ${s.name} right on the board`} disabled={last} onClick={() => onMove(1)} className="h-5 w-7 rounded text-xs text-muted hover:bg-canvas disabled:opacity-30">
              ▼
            </button>
          </span>
        )}
        <span className="flex min-w-40 flex-1 items-center gap-2.5 font-medium">
          <span className="h-3 w-3 rounded-full" style={{ background: s.color }} />
          {s.name}
        </span>
        <Badge tone={catTone[s.category]}>{categoryLabel[s.category]}</Badge>
        <span className="w-20 text-[13px] text-ink-2">
          {s.task_count} {s.task_count === 1 ? "task" : "tasks"}
        </span>
        {canEdit && (
          <span className="flex gap-1.5">
            <Button size="sm" onClick={() => (setEditing(!editing), setDeleting(false))}>
              {editing ? "Close" : "Edit"}
            </Button>
            <Button size="sm" variant="danger" onClick={() => (setDeleting(!deleting), setEditing(false))}>
              Delete
            </Button>
          </span>
        )}
      </div>
      {editing && (
        <form
          className="flex flex-col gap-3 rounded-xl bg-canvas p-3"
          onSubmit={async (e: FormEvent) => {
            e.preventDefault();
            if (await onSave(v)) setEditing(false);
          }}
        >
          <GroupFields v={v} set={setV} />
          <div className="flex justify-end">
            <Button type="submit" variant="primary" size="sm">
              Save group
            </Button>
          </div>
        </form>
      )}
      {deleting && (
        <div className="flex flex-wrap items-center gap-2 rounded-xl bg-warn-soft p-3 text-[13px] text-warn">
          {s.task_count > 0 ? (
            <>
              <span>
                Move its {s.task_count} {s.task_count === 1 ? "task" : "tasks"} to
              </span>
              <select aria-label="Move tasks to" className="h-9 rounded-lg border border-line-2 bg-white px-2 text-[13px] text-ink" value={moveTo} onChange={(e) => setMoveTo(e.target.value)}>
                {others.map((o) => (
                  <option key={o.id} value={o.id}>
                    {o.name}
                  </option>
                ))}
              </select>
              <span>and delete &quot;{s.name}&quot;?</span>
            </>
          ) : (
            <span>Delete &quot;{s.name}&quot;? It has no tasks.</span>
          )}
          <Button size="sm" variant="danger" onClick={() => onDelete(s.task_count > 0 ? moveTo : "")}>
            Delete group
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setDeleting(false)}>
            Keep it
          </Button>
        </div>
      )}
    </li>
  );
}

function AddGroup({ onAdd }: { onAdd: (b: GroupBody) => Promise<boolean> }) {
  const empty: GroupBody = { name: "", color: "#0E7490", category: "active" };
  const [v, setV] = useState(empty);
  return (
    <form
      className="mt-3 flex flex-col gap-3 border-t border-[#eef0f3] pt-4"
      onSubmit={async (e) => {
        e.preventDefault();
        if (await onAdd(v)) setV(empty);
      }}
    >
      <h3 className="text-sm font-semibold">Add a group</h3>
      <GroupFields v={v} set={setV} />
      <p className="text-xs text-muted">A new group goes just before the first Done group.</p>
      <div>
        <Button type="submit" variant="primary" disabled={!v.name.trim()}>
          Add group
        </Button>
      </div>
    </form>
  );
}
