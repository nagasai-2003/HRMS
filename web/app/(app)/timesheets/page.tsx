"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { api } from "@/lib/api";
import { addDays, dayLabel, hm, modeLabel } from "@/lib/format";
import { Badge, Button, Card, cx, Empty, inputCls, Notice, PageHeader, statusTone } from "@/components/ui";

type Entry = { id?: string; day: string; project_id: string; task_ref: string; minutes: number; note: string };
type Day = { day: string; attended_min: number; logged_min: number; mode: string; holiday: string; flag: "" | "over" | "no_attendance"; future: boolean };
type Week = {
  week_start: string;
  week_end: string;
  today: string;
  person: { id: string; name: string; title: string };
  status: "draft" | "submitted" | "approved" | "rejected";
  sheet: { id: string; status: string; submitted_at: string | null; decided_at: string | null; decided_by_name: string; decision_note: string } | null;
  entries: (Entry & { project_code: string; project_name: string })[];
  days: Day[];
  projects: { id: string; code: string; name: string; active: boolean }[];
  can_edit: boolean;
  can_submit: boolean;
  can_recall: boolean;
  can_decide: boolean;
};
type Row = { key: string; day: string; project_id: string; task_ref: string; hours: string; note: string };

const statusText = { draft: "Draft", submitted: "Waiting for approval", approved: "Approved", rejected: "Sent back" };

let keySeq = 0;
const newKey = () => `r${++keySeq}`;

function toRows(w: Week): Row[] {
  return w.entries.map((e) => ({ key: newKey(), day: e.day, project_id: e.project_id, task_ref: e.task_ref, hours: String(+(e.minutes / 60).toFixed(2)), note: e.note }));
}

// "7.5", "7:30" and "7h30" all mean seven and a half hours.
function parseHours(v: string): number | null {
  const t = v.trim().toLowerCase();
  if (!t) return null;
  const m = t.match(/^(\d{1,2})\s*(?:[:h]\s*(\d{1,2})\s*m?)?$/);
  if (m && m[2] !== undefined) return Number(m[1]) * 60 + Number(m[2]);
  const n = Number(t.replace(",", "."));
  if (!Number.isFinite(n)) return null;
  return Math.round(n * 60);
}

function TimesheetInner() {
  const params = useSearchParams();
  const userId = params.get("user_id") ?? "";
  const [start, setStart] = useState(params.get("start") ?? "");
  const [week, setWeek] = useState<Week | null>(null);
  const [rows, setRows] = useState<Row[]>([]);
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState("");
  const [note, setNote] = useState("");
  const [history, setHistory] = useState<{ id: string; week_start: string; status: string; total_minutes: number }[]>([]);

  const apply = useCallback((w: Week) => {
    setWeek(w);
    setStart(w.week_start);
    setRows(toRows(w));
    setDirty(false);
  }, []);

  const load = useCallback(
    async (s: string) => {
      setError("");
      const q = new URLSearchParams();
      if (s) q.set("start", s);
      if (userId) q.set("user_id", userId);
      apply(await api<Week>(`/timesheets/week?${q}`));
    },
    [userId, apply],
  );

  useEffect(() => {
    load(params.get("start") ?? "").catch((e) => setError(e.message));
    if (!userId) api<{ timesheets: typeof history }>("/timesheets").then((r) => setHistory(r.timesheets)).catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [userId]);

  const parsed = useMemo(() => rows.map((r) => ({ ...r, minutes: parseHours(r.hours) })), [rows]);
  const loggedByDay = useMemo(() => {
    const m: Record<string, number> = {};
    for (const r of parsed) if (r.minutes) m[r.day] = (m[r.day] ?? 0) + r.minutes;
    return m;
  }, [parsed]);

  if (!week) return error ? <Notice tone="bad">{error}</Notice> : <p className="text-sm text-muted">Loading…</p>;

  const own = !userId || userId === week.person.id;
  const editable = week.can_edit;
  const openDays = week.days.filter((d) => !d.future);
  const totalLogged = Object.values(loggedByDay).reduce((a, b) => a + b, 0);
  const totalAttended = week.days.reduce((a, d) => a + d.attended_min, 0);
  const invalid = parsed.some((r) => !r.project_id || r.minutes == null || r.minutes < 1 || r.minutes > 1440);

  function update(key: string, patch: Partial<Row>) {
    setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));
    setDirty(true);
    setSaved("");
  }
  function addRow(day?: string) {
    const d = day ?? openDays.at(-1)?.day ?? week!.week_start;
    const last = rows.at(-1);
    setRows((rs) => [...rs, { key: newKey(), day: d, project_id: last?.project_id ?? week!.projects.find((p) => p.active)?.id ?? "", task_ref: "", hours: "", note: "" }]);
    setDirty(true);
  }

  async function save(): Promise<boolean> {
    setBusy(true);
    setError("");
    try {
      const w = await api<Week>("/timesheets/week", {
        method: "PUT",
        body: { start: week!.week_start, entries: parsed.map((r) => ({ day: r.day, project_id: r.project_id, task_ref: r.task_ref, minutes: r.minutes ?? 0, note: r.note })) },
      });
      apply(w);
      setSaved("Saved");
      return true;
    } catch (e) {
      setError((e as Error).message);
      return false;
    } finally {
      setBusy(false);
    }
  }

  async function post(path: string, body: unknown = {}) {
    setBusy(true);
    setError("");
    try {
      apply(await api<Week>(path, { body }));
      if (own) api<{ timesheets: typeof history }>("/timesheets").then((r) => setHistory(r.timesheets));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function submit() {
    if (dirty && !(await save())) return;
    const id = week!.sheet?.id;
    if (id) await post(`/timesheets/${id}/submit`);
  }

  const flagged = week.days.filter((d) => {
    const logged = editable ? loggedByDay[d.day] ?? 0 : d.logged_min;
    return logged > 0 && (d.attended_min === 0 || logged > d.attended_min + 30);
  });

  return (
    <>
      <PageHeader
        eyebrow={own ? `Week of ${dayLabel(week.week_start, { day: "numeric", month: "long" })} to ${dayLabel(week.week_end, { day: "numeric", month: "long", year: "numeric" })}` : `${week.person.name} · ${week.person.title}`}
        title={own ? "Timesheet" : `Timesheet: ${week.person.name}`}
        actions={
          <>
            {!own && <Link href="/team/timesheets" className="mr-2 text-sm font-semibold text-brand">Back to approvals</Link>}
            <Button onClick={() => load(addDays(start, -7))} aria-label="Previous week">← Previous</Button>
            <Button onClick={() => load("")}>This week</Button>
            <Button onClick={() => load(addDays(start, 7))} disabled={addDays(start, 7) > week.today} aria-label="Next week">Next →</Button>
          </>
        }
      />

      {error && <Notice tone="bad">{error}</Notice>}
      {week.status === "rejected" && week.sheet && (
        <Notice tone="bad">
          <b>Sent back by {week.sheet.decided_by_name}:</b> {week.sheet.decision_note} Fix it and submit again.
        </Notice>
      )}
      {week.status === "approved" && week.sheet && <Notice tone="ok">Approved by {week.sheet.decided_by_name}{week.sheet.decision_note ? `: ${week.sheet.decision_note}` : "."}</Notice>}

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <div className="flex flex-col gap-1 rounded-2xl border border-line bg-white p-4">
          <span className="text-[13px] text-muted">Status</span>
          <span><Badge tone={statusTone(week.status)}>{statusText[week.status]}</Badge></span>
        </div>
        <div className="flex flex-col gap-1 rounded-2xl border border-line bg-white p-4">
          <span className="text-[13px] text-muted">Logged this week</span>
          <span className="font-display text-[26px] font-bold">{hm(editable ? totalLogged : week.days.reduce((a, d) => a + d.logged_min, 0))}</span>
        </div>
        <div className="flex flex-col gap-1 rounded-2xl border border-line bg-white p-4">
          <span className="text-[13px] text-muted">At work (from attendance)</span>
          <span className="font-display text-[26px] font-bold">{hm(totalAttended)}</span>
        </div>
        <div className="flex flex-col gap-1 rounded-2xl border border-line bg-white p-4">
          <span className="text-[13px] text-muted">Days to check</span>
          <span className={cx("font-display text-[26px] font-bold", flagged.length ? "text-warn" : "")}>{flagged.length}</span>
        </div>
      </div>

      <Card title="Day by day" aside="Logged time is compared with check-in to check-out, breaks excluded">
        <div className="grid gap-2 sm:grid-cols-4 lg:grid-cols-7">
          {week.days.map((d) => {
            const logged = editable ? loggedByDay[d.day] ?? 0 : d.logged_min;
            const over = logged > 0 && (d.attended_min === 0 || logged > d.attended_min + 30);
            return (
              <div key={d.day} className={cx("flex flex-col gap-1 rounded-xl border p-3 text-sm", over ? "border-[#f5d7a8] bg-warn-soft" : "border-line", d.future && "opacity-50")}>
                <span className="text-xs font-semibold text-muted">{dayLabel(d.day)}</span>
                <span className="font-semibold">{hm(logged)} <span className="font-normal text-muted">logged</span></span>
                <span className="text-xs text-muted">{hm(d.attended_min)} at work</span>
                {d.holiday ? <Badge>{d.holiday}</Badge> : d.mode !== "office" && <Badge tone="violet">{modeLabel[d.mode]}</Badge>}
                {over && <span className="text-xs font-semibold text-warn">{d.attended_min === 0 ? "No attendance this day" : "More than time at work"}</span>}
                {editable && !d.future && (
                  <button type="button" className="mt-1 self-start text-xs font-semibold text-brand" onClick={() => addRow(d.day)}>+ Add time</button>
                )}
              </div>
            );
          })}
        </div>
      </Card>

      <Card title="Entries" aside={editable ? "Hours can be 7.5 or 7:30" : undefined}>
        {rows.length === 0 && !editable ? (
          <Empty>No time logged this week.</Empty>
        ) : (
          <div className="flex flex-col gap-2">
            <div className="hidden grid-cols-[130px_minmax(0,1fr)_100px_80px_minmax(0,1.2fr)_40px] gap-2 px-1 text-xs font-semibold text-muted lg:grid">
              <span>Day</span><span>Project</span><span>Task</span><span>Hours</span><span>What you did</span><span />
            </div>
            {parsed.map((r) => {
              const bad = r.hours.trim() !== "" && (r.minutes == null || r.minutes < 1 || r.minutes > 1440);
              return (
                <div key={r.key} className="grid grid-cols-2 gap-2 rounded-xl border border-line p-2 lg:grid-cols-[130px_minmax(0,1fr)_100px_80px_minmax(0,1.2fr)_40px] lg:border-0 lg:p-0">
                  <select aria-label="Day" disabled={!editable} className={cx(inputCls, "w-full min-w-0")} value={r.day} onChange={(e) => update(r.key, { day: e.target.value })}>
                    {openDays.map((d) => <option key={d.day} value={d.day}>{dayLabel(d.day)}</option>)}
                  </select>
                  <select aria-label="Project" disabled={!editable} className={cx(inputCls, "w-full min-w-0")} value={r.project_id} onChange={(e) => update(r.key, { project_id: e.target.value })}>
                    <option value="">Pick a project</option>
                    {week.projects.map((p) => <option key={p.id} value={p.id} disabled={!p.active}>{p.code} · {p.name}</option>)}
                  </select>
                  <input aria-label="Task" disabled={!editable} className={cx(inputCls, "w-full min-w-0")} placeholder="FE-142" maxLength={40} value={r.task_ref} onChange={(e) => update(r.key, { task_ref: e.target.value })} />
                  <input aria-label="Hours" aria-invalid={bad} disabled={!editable} inputMode="decimal" className={cx(inputCls, "w-full min-w-0", bad && "border-bad")} placeholder="0" value={r.hours} onChange={(e) => update(r.key, { hours: e.target.value })} />
                  <input aria-label="What you did" disabled={!editable} className={cx(inputCls, "col-span-2 w-full min-w-0 lg:col-span-1")} maxLength={300} value={r.note} onChange={(e) => update(r.key, { note: e.target.value })} />
                  {editable ? (
                    <button type="button" aria-label="Remove row" className="h-10 rounded-[10px] text-lg text-muted hover:bg-bad-soft hover:text-bad" onClick={() => { setRows((rs) => rs.filter((x) => x.key !== r.key)); setDirty(true); }}>×</button>
                  ) : <span />}
                </div>
              );
            })}
            {editable && (
              <div className="flex flex-wrap items-center gap-2 pt-2">
                <Button onClick={() => addRow()}>+ Add row</Button>
                <span className="flex-1" />
                {saved && !dirty && <span className="text-sm text-ok">{saved}</span>}
                {dirty && <span className="text-sm text-muted">Unsaved changes</span>}
                <Button onClick={save} disabled={busy || !dirty || invalid}>Save draft</Button>
                <Button variant="primary" onClick={submit} disabled={busy || invalid || rows.length === 0}>Submit for approval</Button>
              </div>
            )}
          </div>
        )}
        {week.can_recall && (
          <div className="mt-4 flex items-center gap-3 border-t border-line pt-4 text-sm text-muted">
            Waiting for your manager. Need to change something?
            <Button size="sm" onClick={() => post(`/timesheets/${week.sheet!.id}/recall`)} disabled={busy}>Recall to edit</Button>
          </div>
        )}
      </Card>

      {week.can_decide && week.sheet && (
        <Card title="Your decision">
          {flagged.length > 0 && (
            <div className="mb-3">
              <Notice tone="warn">{flagged.length} day{flagged.length > 1 ? "s have" : " has"} more time logged than time at work. Check before approving.</Notice>
            </div>
          )}
          <div className="flex flex-col gap-3">
            <input className={inputCls} placeholder="Note to the person (needed to send back)" maxLength={500} value={note} onChange={(e) => setNote(e.target.value)} aria-label="Decision note" />
            <div className="flex gap-2">
              <Button variant="primary" disabled={busy} onClick={() => post(`/timesheets/${week.sheet!.id}/decide`, { approve: true, note })}>Approve</Button>
              <Button variant="danger" disabled={busy || !note.trim()} onClick={() => post(`/timesheets/${week.sheet!.id}/decide`, { approve: false, note })}>Send back</Button>
            </div>
          </div>
        </Card>
      )}

      {own && history.length > 0 && (
        <Card title="Recent weeks">
          <div className="flex flex-col">
            {history.map((h) => (
              <button key={h.id} type="button" onClick={() => load(h.week_start)} className="flex items-center justify-between gap-3 border-t border-[#eef0f3] py-2.5 text-left text-sm first:border-0 hover:bg-canvas">
                <span className="font-semibold">Week of {dayLabel(h.week_start, { day: "numeric", month: "short", year: "numeric" })}</span>
                <span className="flex items-center gap-3"><span className="text-muted">{hm(h.total_minutes)}</span><Badge tone={statusTone(h.status)}>{statusText[h.status as keyof typeof statusText]}</Badge></span>
              </button>
            ))}
          </div>
        </Card>
      )}
    </>
  );
}

export default function TimesheetPage() {
  return (
    <Suspense fallback={<p className="text-sm text-muted">Loading…</p>}>
      <TimesheetInner />
    </Suspense>
  );
}
