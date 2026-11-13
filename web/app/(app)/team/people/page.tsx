"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { RoleGate, useMe } from "@/components/shell";
import { Badge, Card, Empty, inputCls, Notice, PageHeader, Table } from "@/components/ui";

type Person = { id: string; name: string; title: string; role: string; work_policy: string; teams: string[] };

export default function PeoplePage() {
  return (
    <RoleGate allow={(m) => m.can.team_views}>
      <Inner />
    </RoleGate>
  );
}

function Inner() {
  const me = useMe();
  const [people, setPeople] = useState<Person[] | null>(null);
  const [q, setQ] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    api<{ people: Person[] }>("/team/people").then((r) => setPeople(r.people)).catch((e) => setError(e.message));
  }, []);
  const rows = (people ?? []).filter((p) => !q || `${p.name} ${p.title} ${p.teams.join(" ")}`.toLowerCase().includes(q.toLowerCase()));
  return (
    <>
      <PageHeader eyebrow={me.role === "manager" ? "People in the teams you manage" : "Everyone in the company"} title="People" />
      {error && <Notice tone="bad">{error}</Notice>}
      <Card aside={<input type="search" aria-label="Search people" placeholder="Search" className={inputCls} value={q} onChange={(e) => setQ(e.target.value)} />} title={`${rows.length} people`}>
        {!people ? <p className="text-sm text-muted">Loading…</p> : rows.length === 0 ? <Empty>No one matches.</Empty> : (
          <Table head={["Name", "Teams", "Work policy", "Role", ""]} minWidth={640}>
            {rows.map((p) => (
              <tr key={p.id}>
                <td><div className="flex flex-col"><span className="font-semibold">{p.name}</span><span className="text-xs text-muted">{p.title}</span></div></td>
                <td>{p.teams.join(", ") || "–"}</td>
                <td className="capitalize">{p.work_policy}</td>
                <td><Badge tone={p.role === "employee" ? "neutral" : "info"}>{p.role === "hr" ? "HR" : p.role[0].toUpperCase() + p.role.slice(1)}</Badge></td>
                <td className="text-right whitespace-nowrap">
                  <Link className="mr-4 text-[13px] font-semibold text-brand" href={`/dashboard?user=${p.id}`}>Dashboard</Link>
                  <Link className="text-[13px] font-semibold text-brand" href={`/timesheets?user_id=${p.id}`}>Timesheet</Link>
                </td>
              </tr>
            ))}
          </Table>
        )}
      </Card>
    </>
  );
}
