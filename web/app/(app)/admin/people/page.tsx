"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { weekdays } from "@/lib/format";
import { RoleGate, useMe } from "@/components/shell";
import { Badge, Button, Card, cx, Empty, Field, inputCls, Notice, PageHeader, Table } from "@/components/ui";

type Person = { id: string; email: string; name: string; title: string; role: string; work_policy: string; remote_weekdays: number[]; office_id: string | null; active: boolean };
type Team = { id: string; name: string; key: string; members: { user_id: string; is_manager: boolean }[] };
type Data = { people: Person[]; teams: Team[]; offices: { id: string; name: string }[] };

const blank: Person & { password: string } = { id: "", email: "", name: "", title: "", role: "employee", work_policy: "office", remote_weekdays: [], office_id: null, active: true, password: "" };
const roleText: Record<string, string> = { employee: "Employee", manager: "Manager", hr: "HR", admin: "Admin" };

export default function AdminPeoplePage() {
  return (
    <RoleGate allow={(m) => m.can.admin}>
      <Inner />
    </RoleGate>
  );
}

function Inner() {
  const me = useMe();
  const [data, setData] = useState<Data | null>(null);
  const [teamId, setTeamId] = useState("");
  const [edit, setEdit] = useState<(Person & { password: string }) | null>(null);
  const [newTeam, setNewTeam] = useState("");
  const [newKey, setNewKey] = useState("");
  const [teamEdit, setTeamEdit] = useState<{ name: string; key: string } | null>(null);
  const [addUser, setAddUser] = useState("");
  const [error, setError] = useState("");
  const [ok, setOk] = useState("");

  const load = useCallback(async () => {
    const d = await api<Data>("/admin/people");
    setData(d);
    setTeamId((t) => t || d.teams[0]?.id || "");
  }, []);
  useEffect(() => {
    load().catch((e) => setError(e.message));
  }, [load]);

  async function run(fn: () => Promise<unknown>, done?: string) {
    setError("");
    setOk("");
    try {
      await fn();
      await load();
      if (done) setOk(done);
      return true;
    } catch (e) {
      setError((e as Error).message);
      return false;
    }
  }

  if (!data) return error ? <Notice tone="bad">{error}</Notice> : <p className="text-sm text-muted">Loading…</p>;
  const byId = Object.fromEntries(data.people.map((p) => [p.id, p]));
  const team = data.teams.find((t) => t.id === teamId);
  const notInTeam = data.people.filter((p) => p.active && !team?.members.some((m) => m.user_id === p.id));

  async function savePerson(e: FormEvent) {
    e.preventDefault();
    if (!edit) return;
    const body = { email: edit.email, name: edit.name, title: edit.title, role: edit.role, work_policy: edit.work_policy, remote_weekdays: edit.remote_weekdays, office_id: edit.office_id || null, active: edit.active, password: edit.password };
    const done = await run(
      () => (edit.id ? api(`/admin/people/${edit.id}`, { method: "PATCH", body: { ...body, email: undefined } }) : api("/admin/people", { body })),
      edit.id ? `${edit.name} updated.` : `${edit.name} added. Share the first password with them in person.`,
    );
    if (done) setEdit(null);
  }

  return (
    <>
      <PageHeader
        eyebrow={`${data.people.filter((p) => p.active).length} active people in ${data.teams.length} teams`}
        title="Teams and people"
        actions={<Button variant="primary" onClick={() => setEdit({ ...blank })}>+ Add person</Button>}
      />
      {error && <Notice tone="bad">{error}</Notice>}
      {ok && <Notice tone="ok">{ok}</Notice>}

      {edit && (
        <Card title={edit.id ? `Edit ${edit.name}` : "Add a person"}>
          <form onSubmit={savePerson} className="grid gap-4 md:grid-cols-3">
            <Field label="Full name"><input className={inputCls} required value={edit.name} onChange={(e) => setEdit({ ...edit, name: e.target.value })} /></Field>
            <Field label="Work email"><input className={inputCls} type="email" required disabled={!!edit.id} value={edit.email} onChange={(e) => setEdit({ ...edit, email: e.target.value })} /></Field>
            <Field label="Job title"><input className={inputCls} value={edit.title} onChange={(e) => setEdit({ ...edit, title: e.target.value })} /></Field>
            <Field label="App access" hint="Manager: sees and approves their own teams. HR: whole company, decides WFH. Admin: everything.">
              <select className={inputCls} value={edit.role} disabled={edit.id === me.id} onChange={(e) => setEdit({ ...edit, role: e.target.value })}>
                {Object.entries(roleText).map(([v, l]) => <option key={v} value={v}>{l}</option>)}
              </select>
            </Field>
            <Field label="Work policy">
              <select className={inputCls} value={edit.work_policy} onChange={(e) => setEdit({ ...edit, work_policy: e.target.value, remote_weekdays: e.target.value === "hybrid" ? (edit.remote_weekdays.length ? edit.remote_weekdays : [3]) : [] })}>
                <option value="office">Office (WFH needs approval)</option>
                <option value="hybrid">Hybrid (fixed remote days)</option>
                <option value="remote">Fully remote</option>
              </select>
            </Field>
            <Field label="Home office">
              <select className={inputCls} value={edit.office_id ?? ""} onChange={(e) => setEdit({ ...edit, office_id: e.target.value || null })}>
                <option value="">None</option>
                {data.offices.map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}
              </select>
            </Field>
            {edit.work_policy === "hybrid" && (
              <fieldset className="flex flex-col gap-1.5 md:col-span-3">
                <legend className="text-[13px] font-semibold text-ink-2">Remote days</legend>
                <div className="flex flex-wrap gap-2">
                  {weekdays.map((w, i) => {
                    const on = edit.remote_weekdays.includes(i + 1);
                    return (
                      <button key={w} type="button" aria-pressed={on} onClick={() => setEdit({ ...edit, remote_weekdays: on ? edit.remote_weekdays.filter((d) => d !== i + 1) : [...edit.remote_weekdays, i + 1] })}
                        className={cx("min-h-9 rounded-full border px-3.5 text-[13px] font-semibold", on ? "border-violet bg-violet text-white" : "border-line-2 bg-white")}>
                        {w}
                      </button>
                    );
                  })}
                </div>
              </fieldset>
            )}
            <Field label={edit.id ? "New password (leave empty to keep)" : "First password"} hint="At least 10 characters. Signs them out everywhere when changed.">
              <input className={inputCls} type="password" autoComplete="new-password" minLength={10} required={!edit.id} value={edit.password} onChange={(e) => setEdit({ ...edit, password: e.target.value })} />
            </Field>
            {edit.id && (
              <label className="flex items-center gap-2 self-end pb-2 text-sm">
                <input type="checkbox" className="h-4 w-4" checked={edit.active} disabled={edit.id === me.id} onChange={(e) => setEdit({ ...edit, active: e.target.checked })} />
                Active (can sign in)
              </label>
            )}
            <div className="flex gap-2 md:col-span-3">
              <Button type="submit" variant="primary">{edit.id ? "Save" : "Add person"}</Button>
              <Button variant="ghost" onClick={() => setEdit(null)}>Cancel</Button>
            </div>
          </form>
        </Card>
      )}

      <div className="grid gap-5 lg:grid-cols-[280px_1fr]">
        <div className="flex flex-col gap-2.5">
          {data.teams.map((t) => {
            const mgrs = t.members.filter((m) => m.is_manager).map((m) => byId[m.user_id]?.name).filter(Boolean);
            return (
              <button key={t.id} type="button" onClick={() => { setTeamId(t.id); setTeamEdit(null); }} aria-pressed={t.id === teamId}
                className={cx("flex flex-col gap-1 rounded-2xl border-2 bg-white px-4 py-3 text-left", t.id === teamId ? "border-brand" : "border-line")}>
                <span className="flex justify-between gap-2"><span className="font-semibold">{t.name} <span className="ml-1 rounded bg-canvas px-1.5 py-0.5 text-[11px] font-semibold text-ink-2">{t.key}</span></span><span className="text-xs text-muted">{t.members.length} people</span></span>
                <span className="text-xs text-muted">Manager: {mgrs.join(", ") || "none"}</span>
              </button>
            );
          })}
          <form className="flex gap-2" onSubmit={async (e) => { e.preventDefault(); if (await run(() => api("/admin/teams", { body: { name: newTeam, key: newKey } }), `${newTeam} created.`)) { setNewTeam(""); setNewKey(""); } }}>
            <input className={inputCls + " min-w-0 flex-1"} placeholder="New team name" aria-label="New team name" value={newTeam} onChange={(e) => setNewTeam(e.target.value)} />
            <input className={inputCls + " w-20 uppercase"} placeholder="Key" aria-label="Task key for the new team, like FE" maxLength={6} value={newKey} onChange={(e) => setNewKey(e.target.value.toUpperCase())} />
            <Button type="submit" disabled={!newTeam.trim()}>Create</Button>
          </form>
          <p className="text-xs leading-relaxed text-muted">A person can be in more than one team. Team tasks and channels stay private to the team. The key starts every task number (FE-142); leave it empty to get one from the name.</p>
        </div>

        <Card
          title={team ? `${team.name} team · ${team.key}` : "Pick a team"}
          aside={team && (
            <span className="flex gap-4">
              <button type="button" className="font-semibold text-brand" onClick={() => setTeamEdit(teamEdit ? null : { name: team.name, key: team.key })}>{teamEdit ? "Close" : "Rename"}</button>
              <button type="button" className="font-semibold text-bad" onClick={() => confirm(`Delete the ${team.name} team? People stay, but its task board and every task on it go too.`) && run(() => api(`/admin/teams/${team.id}`, { method: "DELETE" }), `${team.name} deleted.`).then(() => setTeamId(""))}>Delete team</button>
            </span>
          )}
        >
          {team && (
            <>
              {teamEdit && (
                <form className="mb-4 flex flex-wrap items-end gap-2 rounded-xl bg-canvas p-3" onSubmit={async (e) => { e.preventDefault(); if (await run(() => api(`/admin/teams/${team.id}`, { method: "PATCH", body: teamEdit }), `${teamEdit.name} saved.`)) setTeamEdit(null); }}>
                  <Field label="Team name"><input className={inputCls + " w-56"} value={teamEdit.name} onChange={(e) => setTeamEdit({ ...teamEdit, name: e.target.value })} /></Field>
                  <Field label="Task key" hint="Changing it renames every task (FE-142 → WEB-142)."><input className={inputCls + " w-28 uppercase"} maxLength={6} value={teamEdit.key} onChange={(e) => setTeamEdit({ ...teamEdit, key: e.target.value.toUpperCase() })} /></Field>
                  <Button type="submit" variant="primary">Save</Button>
                </form>
              )}
              {team.members.length === 0 ? <Empty>No one in this team yet.</Empty> : (
                <Table head={["Person", "App access", "Team manager", ""]} minWidth={560}>
                  {team.members.map((m) => {
                    const p = byId[m.user_id];
                    return (
                      <tr key={m.user_id}>
                        <td><div className="flex flex-col"><span className="font-semibold">{p?.name}</span><span className="text-xs text-muted">{p?.title}</span></div></td>
                        <td><Badge tone={p?.role === "employee" ? "neutral" : "info"}>{roleText[p?.role ?? "employee"]}</Badge></td>
                        <td>
                          <label className="flex items-center gap-2 text-sm">
                            <input type="checkbox" className="h-4 w-4" checked={m.is_manager}
                              onChange={(e) => run(() => api(`/admin/teams/${team.id}/members/${m.user_id}`, { method: "PUT", body: { is_manager: e.target.checked } }), e.target.checked ? `${p?.name} now manages ${team.name}.` : undefined)} />
                            Manages this team
                          </label>
                        </td>
                        <td className="text-right"><Button size="sm" variant="ghost" onClick={() => run(() => api(`/admin/teams/${team.id}/members/${m.user_id}`, { method: "DELETE" }))}>Remove</Button></td>
                      </tr>
                    );
                  })}
                </Table>
              )}
              <div className="mt-4 flex flex-wrap gap-2 border-t border-line pt-4">
                <select className={inputCls + " min-w-56"} aria-label="Person to add" value={addUser} onChange={(e) => setAddUser(e.target.value)}>
                  <option value="">Add someone to {team.name}…</option>
                  {notInTeam.map((p) => <option key={p.id} value={p.id}>{p.name} · {p.title}</option>)}
                </select>
                <Button disabled={!addUser} onClick={async () => { if (await run(() => api(`/admin/teams/${team.id}/members/${addUser}`, { method: "PUT", body: { is_manager: false } }))) setAddUser(""); }}>Add</Button>
              </div>
              <p className="mt-2 text-xs text-muted">Ticking &quot;Manages this team&quot; gives an employee Manager access automatically.</p>
            </>
          )}
        </Card>
      </div>

      <Card title="Everyone">
        <Table head={["Name", "Email", "Access", "Work policy", "Status", ""]} minWidth={820}>
          {data.people.map((p) => (
            <tr key={p.id} className={p.active ? "" : "opacity-60"}>
              <td><div className="flex flex-col"><span className="font-semibold">{p.name}</span><span className="text-xs text-muted">{p.title}</span></div></td>
              <td className="text-ink-2">{p.email}</td>
              <td><Badge tone={p.role === "employee" ? "neutral" : "info"}>{roleText[p.role]}</Badge></td>
              <td>{p.work_policy === "hybrid" ? `Hybrid · ${p.remote_weekdays.map((d) => weekdays[d - 1]).join(", ")}` : p.work_policy === "remote" ? "Remote" : "Office"}</td>
              <td>{p.active ? <Badge tone="ok">Active</Badge> : <Badge>Deactivated</Badge>}</td>
              <td className="text-right"><Button size="sm" variant="ghost" onClick={() => setEdit({ ...p, password: "" })}>Edit</Button></td>
            </tr>
          ))}
        </Table>
      </Card>
    </>
  );
}
