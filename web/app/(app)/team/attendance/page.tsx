"use client";

import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { clock, dayLabel, hm, modeLabel } from "@/lib/format";
import { RoleGate } from "@/components/shell";
import { Badge, Button, Card, cx, Empty, inputCls, Notice, PageHeader, Table, Tone } from "@/components/ui";

type Person = {
  id: string; name: string; title: string; teams: string[]; mode: string; status: string; bucket: string; break_reason: string;
  first_in: string | null; last_out: string | null; worked_min: number; break_min: number; late: boolean; blocked: number;
  ip?: string; office?: string; distance_m?: number; auto_closed: boolean;
};
type Data = {
  day: string; today: string; holiday: string; people: Person[]; counts: Record<string, number>;
  blocked: { at: string; user_name: string; action: string; message: string; ip: string }[];
  teams: { id: string; name: string }[];
};

const tiles: { key: string; label: string; color: string }[] = [
  { key: "office", label: "In office", color: "#2f5bd3" },
  { key: "on_duty", label: "On-duty", color: "#0e7490" },
  { key: "wfh", label: "WFH", color: "#15803d" },
  { key: "remote", label: "Remote / hybrid", color: "#7c3aed" },
  { key: "break", label: "On break now", color: "#d97706" },
  { key: "not_in", label: "Not checked in", color: "#b42318" },
];

const statusBadge: Record<string, [Tone, string]> = { in: ["ok", "Working"], break: ["warn", "Break"], done: ["info", "Checked out"], out: ["bad", "Not in"] };
const actionText: Record<string, string> = { check_in: "Check in", break_start: "Start break", break_end: "End break", check_out: "Check out" };

export default function TeamAttendancePage() {
  return (
    <RoleGate allow={(m) => m.can.team_views}>
      <Inner />
    </RoleGate>
  );
}

function Inner() {
  const [data, setData] = useState<Data | null>(null);
  const [day, setDay] = useState("");
  const [team, setTeam] = useState("");
  const [filter, setFilter] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async (d: string, t: string) => {
    const q = new URLSearchParams();
    if (d) q.set("day", d);
    if (t) q.set("team_id", t);
    const r = await api<Data>(`/team/attendance?${q}`);
    setData(r);
    setDay(r.day);
  }, []);

  useEffect(() => {
    load("", "").catch((e) => setError(e.message));
  }, [load]);

  // Live: refresh every minute when looking at today.
  useEffect(() => {
    if (!data || data.day !== data.today) return;
    const t = setInterval(() => load(day, team).catch(() => {}), 60000);
    return () => clearInterval(t);
  }, [data, day, team, load]);

  if (!data) return error ? <Notice tone="bad">{error}</Notice> : <p className="text-sm text-muted">Loading…</p>;
  const rows = data.people.filter((p) => !filter || p.bucket === filter);

  return (
    <>
      <PageHeader
        eyebrow={`${data.day === data.today ? "Live · refreshes every minute" : dayLabel(data.day, { weekday: "long", day: "numeric", month: "long" })}${data.holiday ? ` · ${data.holiday}` : ""}`}
        title="Team attendance"
        actions={
          <>
            <label className="flex flex-col gap-1 text-xs font-semibold text-muted">Day
              <input type="date" className={inputCls} value={day} max={data.today} onChange={(e) => { setDay(e.target.value); load(e.target.value, team); }} />
            </label>
            {data.teams.length > 1 && (
              <label className="flex flex-col gap-1 text-xs font-semibold text-muted">Team
                <select className={inputCls} value={team} onChange={(e) => { setTeam(e.target.value); load(day, e.target.value); }}>
                  <option value="">All my teams</option>
                  {data.teams.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
                </select>
              </label>
            )}
          </>
        }
      />
      {error && <Notice tone="bad">{error}</Notice>}

      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        {tiles.map((t) => (
          <button
            key={t.key}
            type="button"
            aria-pressed={filter === t.key}
            onClick={() => setFilter(filter === t.key ? "" : t.key)}
            className={cx("flex flex-col gap-1 rounded-2xl border-2 bg-white px-4 py-3.5 text-left", filter === t.key ? "border-brand" : "border-line")}
          >
            <span className="flex items-center gap-2 text-[13px] text-ink-2"><span className="h-2.5 w-2.5 rounded-full" style={{ background: t.color }} />{t.label}</span>
            <span className="font-display text-[26px] font-bold">{data.counts[t.key] ?? 0}</span>
          </button>
        ))}
      </div>

      <Card aside={filter ? <Button size="sm" variant="ghost" onClick={() => setFilter("")}>Show everyone</Button> : `${data.people.length} people`} title="People">
        {rows.length === 0 ? (
          <Empty>No one here.</Empty>
        ) : (
          <Table head={["Employee", "Mode", "Check in", "Now", "Where", "Network IP", "Worked", ""]} minWidth={980}>
            {rows.map((p) => {
              const [tone, label] = statusBadge[p.status] ?? ["neutral", p.status];
              return (
                <tr key={p.id}>
                  <td><div className="flex flex-col"><span className="font-semibold">{p.name}</span><span className="text-xs text-muted">{p.teams.join(", ")}</span></div></td>
                  <td>{modeLabel[p.mode] ?? p.mode}</td>
                  <td className="whitespace-nowrap">{clock(p.first_in)} {p.late && <Badge tone="warn">Late</Badge>}</td>
                  <td><Badge tone={tone}>{label}{p.status === "break" && p.break_reason ? ` · ${p.break_reason}` : ""}</Badge>{p.auto_closed && <span className="ml-1 text-xs text-bad">auto check-out</span>}</td>
                  <td className="text-ink-2">{p.office ? `${p.office}${p.distance_m != null ? ` · ${p.distance_m} m` : ""}` : p.first_in ? "Recorded" : "–"}{p.blocked > 0 && <div className="text-xs text-bad">{p.blocked} blocked attempt{p.blocked > 1 ? "s" : ""}</div>}</td>
                  <td className="tabular-nums text-ink-2">{p.ip ?? "–"}</td>
                  <td>{p.first_in ? hm(p.worked_min) : "–"}</td>
                  <td><a href={`/dashboard?user=${p.id}`} className="text-[13px] font-semibold text-brand">Dashboard</a></td>
                </tr>
              );
            })}
          </Table>
        )}
      </Card>

      <Card title="Blocked attempts" aside="Refused by the location or network rules">
        {data.blocked.length === 0 ? (
          <Empty>None on this day.</Empty>
        ) : (
          <ul className="flex flex-col">
            {data.blocked.map((b, i) => (
              <li key={i} className="flex flex-wrap items-center gap-3 border-t border-[#eef0f3] py-3 first:border-0">
                <span className="w-12 font-display font-bold">{clock(b.at)}</span>
                <span className="min-w-40 flex-1 font-semibold">{b.user_name} <span className="font-normal text-muted">· {actionText[b.action] ?? b.action}</span></span>
                <span className="flex-[2_1_260px] text-[13px] text-bad">{b.message}</span>
                <span className="text-xs text-muted">{b.ip}</span>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </>
  );
}
