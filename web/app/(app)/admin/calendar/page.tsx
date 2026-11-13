"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { dayLabel } from "@/lib/format";
import { RoleGate } from "@/components/shell";
import { Badge, Button, Card, Empty, Field, inputCls, Notice, PageHeader, Table } from "@/components/ui";

type Project = { id: string; code: string; name: string; active: boolean };

export default function CalendarPage() {
  return (
    <RoleGate allow={(m) => m.can.holidays}>
      <Inner />
    </RoleGate>
  );
}

function Inner() {
  const [holidays, setHolidays] = useState<{ day: string; name: string }[] | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [h, setH] = useState({ day: "", name: "" });
  const [p, setP] = useState({ code: "", name: "" });
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    const [hs, ps] = await Promise.all([api<{ holidays: { day: string; name: string }[] }>("/admin/holidays"), api<{ projects: Project[] }>("/admin/projects")]);
    setHolidays(hs.holidays);
    setProjects(ps.projects);
  }, []);
  useEffect(() => {
    load().catch((e) => setError(e.message));
  }, [load]);

  async function run(fn: () => Promise<unknown>) {
    setError("");
    try {
      await fn();
      await load();
      return true;
    } catch (e) {
      setError((e as Error).message);
      return false;
    }
  }

  async function addHoliday(e: FormEvent) {
    e.preventDefault();
    if (await run(() => api(`/admin/holidays/${h.day}`, { method: "PUT", body: { name: h.name } }))) setH({ day: "", name: "" });
  }
  async function addProject(e: FormEvent) {
    e.preventDefault();
    if (await run(() => api("/admin/projects", { body: p }))) setP({ code: "", name: "" });
  }

  return (
    <>
      <PageHeader eyebrow="HR and Admin" title="Holidays and projects" />
      {error && <Notice tone="bad">{error}</Notice>}
      <div className="grid gap-5 lg:grid-cols-2">
        <Card title="Company holidays" aside="Check-in is closed on these days unless on-duty is approved">
          {!holidays ? <p className="text-sm text-muted">Loading…</p> : holidays.length === 0 ? <Empty>No holidays added.</Empty> : (
            <Table head={["Day", "Holiday", ""]} minWidth={360}>
              {holidays.map((x) => (
                <tr key={x.day}>
                  <td className="whitespace-nowrap font-semibold">{dayLabel(x.day, { weekday: "short", day: "numeric", month: "short", year: "numeric" })}</td>
                  <td>{x.name}</td>
                  <td className="text-right"><Button size="sm" variant="ghost" onClick={() => run(() => api(`/admin/holidays/${x.day}`, { method: "DELETE" }))}>Remove</Button></td>
                </tr>
              ))}
            </Table>
          )}
          <form onSubmit={addHoliday} className="mt-4 grid gap-3 border-t border-line pt-4 sm:grid-cols-[160px_1fr_auto]">
            <Field label="Day"><input className={inputCls} type="date" required value={h.day} onChange={(e) => setH({ ...h, day: e.target.value })} /></Field>
            <Field label="Name"><input className={inputCls} required value={h.name} onChange={(e) => setH({ ...h, name: e.target.value })} /></Field>
            <div className="flex items-end"><Button type="submit" variant="primary">Add</Button></div>
          </form>
        </Card>

        <Card title="Timesheet projects" aside="People log their hours against these">
          <Table head={["Code", "Name", "Status", ""]} minWidth={420}>
            {projects.map((x) => (
              <tr key={x.id} className={x.active ? "" : "opacity-60"}>
                <td className="font-semibold">{x.code}</td>
                <td>{x.name}</td>
                <td>{x.active ? <Badge tone="ok">Open</Badge> : <Badge>Closed</Badge>}</td>
                <td className="text-right">
                  <Button size="sm" variant="ghost" onClick={() => run(() => api(`/admin/projects/${x.id}`, { method: "PATCH", body: { code: x.code, name: x.name, active: !x.active } }))}>{x.active ? "Close" : "Reopen"}</Button>
                </td>
              </tr>
            ))}
          </Table>
          <form onSubmit={addProject} className="mt-4 grid gap-3 border-t border-line pt-4 sm:grid-cols-[140px_1fr_auto]">
            <Field label="Code"><input className={inputCls} required maxLength={20} value={p.code} onChange={(e) => setP({ ...p, code: e.target.value })} /></Field>
            <Field label="Name"><input className={inputCls} required value={p.name} onChange={(e) => setP({ ...p, name: e.target.value })} /></Field>
            <div className="flex items-end"><Button type="submit" variant="primary">Add</Button></div>
          </form>
        </Card>
      </div>
    </>
  );
}
