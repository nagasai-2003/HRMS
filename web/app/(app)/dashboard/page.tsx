"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { Bar, BarChart, CartesianGrid, Cell, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { api } from "@/lib/api";
import { clock, dayLabel, hm, modeLabel } from "@/lib/format";
import { useMe } from "@/components/shell";
import { Card, Empty, Notice, PageHeader, Stat } from "@/components/ui";

type DayRow = { day: string; mode: string; holiday: string; late: boolean; summary: { first_in: string | null; last_out: string | null; worked_min: number; break_min: number; auto_closed: boolean } };
type Stats = {
  month: string;
  days_present: number;
  avg_first_in: string;
  avg_last_out: string;
  avg_worked_min: number;
  avg_break_min: number;
  late_days: number;
  wfh_used: number;
  wfh_quota: number;
  missed_checkouts: number;
  logged_min: number;
  attended_min: number;
  days: DayRow[];
};

function minutesOfDay(iso: string) {
  const [h, m] = clock(iso).split(":").map(Number);
  return h * 60 + m;
}

function DashboardInner() {
  const me = useMe();
  const params = useSearchParams();
  const userId = params.get("user");
  const [data, setData] = useState<{ person: { id: string; name: string; title: string }; stats: Stats } | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    setData(null);
    api<{ person: { id: string; name: string; title: string }; stats: Stats }>(userId ? `/team/people/${userId}/summary` : "/me/summary")
      .then(setData)
      .catch((e) => setError(e.message));
  }, [userId]);

  if (error) return <Notice tone="bad">{error}</Notice>;
  if (!data) return <p className="text-sm text-muted">Loading…</p>;
  const s = data.stats;
  const mine = !userId || userId === me.id;
  const monthName = new Date(`${s.month}-01T00:00:00Z`).toLocaleDateString("en-GB", { month: "long", year: "numeric", timeZone: "UTC" });

  const chart = [...s.days]
    .reverse()
    .filter((d) => d.summary.first_in)
    .map((d) => ({ day: dayLabel(d.day, { day: "numeric", month: "short" }), min: Math.min(180, Math.max(4, minutesOfDay(d.summary.first_in!) - 480)), late: d.late, label: clock(d.summary.first_in) }));

  const modes: Record<string, number> = {};
  for (const d of s.days) if (d.summary.first_in) modes[d.mode] = (modes[d.mode] ?? 0) + 1;

  return (
    <>
      {!mine && (
        <Notice>
          You are looking at {data.person.name}&apos;s dashboard. <Link href="/team/people" className="font-semibold underline">Back to people</Link>
        </Notice>
      )}
      <PageHeader eyebrow={`${monthName} · ${mine ? "your" : data.person.name + "'s"} attendance`} title={mine ? `Hello, ${me.name.split(" ")[0]}` : data.person.name} />

      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        <Stat label="Average login" value={s.avg_first_in || "–"} note={`${s.late_days} late day${s.late_days === 1 ? "" : "s"}`} />
        <Stat label="Average logout" value={s.avg_last_out || "–"} note={s.missed_checkouts ? `${s.missed_checkouts} missed check-out` : "No missed check-outs"} />
        <Stat label="Average a day" value={hm(s.avg_worked_min)} note="Breaks excluded" />
        <Stat label="Average break" value={hm(s.avg_break_min)} note="Per day" />
        <Stat label="Days present" value={s.days_present} note="This month so far" />
        <Stat label="WFH used" value={`${s.wfh_used} of ${s.wfh_quota}`} note="Approved days" />
      </div>

      <div className="grid gap-5 lg:grid-cols-[1.6fr_1fr]">
        <Card title="Login time by day" aside="Orange: after the late mark">
          {chart.length === 0 ? (
            <Empty>No check-ins this month yet.</Empty>
          ) : (
            <div className="h-64">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={chart} margin={{ top: 8, right: 8, left: -8, bottom: 0 }}>
                  <CartesianGrid vertical={false} stroke="#eef0f3" />
                  <XAxis dataKey="day" tick={{ fontSize: 12, fill: "#5b6170" }} tickLine={false} axisLine={false} />
                  <YAxis
                    domain={[0, 180]}
                    ticks={[0, 60, 120, 180]}
                    tickFormatter={(v: number) => `${String(8 + v / 60).padStart(2, "0")}:00`}
                    tick={{ fontSize: 12, fill: "#5b6170" }}
                    tickLine={false}
                    axisLine={false}
                  />
                  <ReferenceLine y={105} stroke="#d97706" strokeDasharray="4 4" />
                  <Tooltip formatter={(_v, _n, p) => [(p?.payload as { label: string }).label, "Checked in"]} cursor={{ fill: "#f4f5f7" }} />
                  <Bar dataKey="min" radius={[4, 4, 0, 0]} maxBarSize={28}>
                    {chart.map((c, i) => <Cell key={i} fill={c.late ? "#d97706" : "#2f5bd3"} />)}
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            </div>
          )}
        </Card>

        <div className="flex flex-col gap-5">
          <Card title="Timesheet vs attendance" aside="This month">
            <div className="flex flex-col gap-3 text-sm">
              <Bar2 label="Logged in timesheets" value={s.logged_min} max={Math.max(s.logged_min, s.attended_min, 1)} color="#2f5bd3" />
              <Bar2 label="At work (attendance)" value={s.attended_min} max={Math.max(s.logged_min, s.attended_min, 1)} color="#15803d" />
              {mine && <Link href="/timesheets" className="text-[13px] font-semibold text-brand">Open this week&apos;s timesheet</Link>}
            </div>
          </Card>
          <Card title="Where you worked" aside="Days this month">
            {Object.keys(modes).length === 0 ? (
              <Empty>Nothing yet.</Empty>
            ) : (
              <ul className="flex flex-col gap-2 text-sm">
                {Object.entries(modes).map(([m, n]) => (
                  <li key={m} className="flex justify-between border-t border-[#eef0f3] pt-2 first:border-0 first:pt-0">
                    <span>{modeLabel[m] ?? m}</span>
                    <b>{n}</b>
                  </li>
                ))}
              </ul>
            )}
          </Card>
        </div>
      </div>
    </>
  );
}

function Bar2({ label, value, max, color }: { label: string; value: number; max: number; color: string }) {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex justify-between"><span>{label}</span><b>{hm(value)}</b></div>
      <div className="h-2.5 rounded bg-[#eef0f3]"><div className="h-2.5 rounded" style={{ width: `${(value / max) * 100}%`, background: color }} /></div>
    </div>
  );
}

export default function DashboardPage() {
  return (
    <Suspense fallback={<p className="text-sm text-muted">Loading…</p>}>
      <DashboardInner />
    </Suspense>
  );
}
