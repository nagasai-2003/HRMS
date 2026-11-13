"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { dayLabel, hm } from "@/lib/format";
import { RoleGate } from "@/components/shell";
import { Badge, Card, Empty, Notice, PageHeader, statusTone, Table, Tabs } from "@/components/ui";

type Sheet = { id: string; user_id: string; user_name: string; week_start: string; status: string; total_minutes: number; attended_minutes: number; submitted_at: string; decision_note: string };

export default function TeamTimesheetsPage() {
  return (
    <RoleGate allow={(m) => m.can.review_timesheets}>
      <Inner />
    </RoleGate>
  );
}

function Inner() {
  const [status, setStatus] = useState<"submitted" | "approved" | "rejected" | "all">("submitted");
  const [rows, setRows] = useState<Sheet[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    setRows(null);
    api<{ timesheets: Sheet[] }>(`/timesheets/review?status=${status}`).then((r) => setRows(r.timesheets)).catch((e) => setError(e.message));
  }, [status]);

  return (
    <>
      <PageHeader eyebrow="Weekly timesheets from the people you manage" title="Timesheet approvals" />
      {error && <Notice tone="bad">{error}</Notice>}
      <Tabs value={status} onChange={setStatus} items={[{ value: "submitted", label: "Waiting" }, { value: "approved", label: "Approved" }, { value: "rejected", label: "Sent back" }, { value: "all", label: "All" }]} />
      <Card>
        {!rows ? (
          <p className="text-sm text-muted">Loading…</p>
        ) : rows.length === 0 ? (
          <Empty>{status === "submitted" ? "Nothing is waiting for you." : "No timesheets here."}</Empty>
        ) : (
          <Table head={["Person", "Week", "Logged", "At work", "Status", ""]} minWidth={720}>
            {rows.map((t) => {
              const over = t.total_minutes > t.attended_minutes + 60;
              return (
                <tr key={t.id}>
                  <td className="font-semibold">{t.user_name}</td>
                  <td>{dayLabel(t.week_start, { day: "numeric", month: "short", year: "numeric" })}</td>
                  <td className={over ? "font-semibold text-warn" : ""}>{hm(t.total_minutes)}</td>
                  <td>{hm(t.attended_minutes)}{over && <div className="text-xs text-warn">Logged more than at work</div>}</td>
                  <td><Badge tone={statusTone(t.status)}>{t.status === "submitted" ? "Waiting" : t.status === "rejected" ? "Sent back" : "Approved"}</Badge></td>
                  <td className="text-right"><Link className="text-[13px] font-semibold text-brand" href={`/timesheets?user_id=${t.user_id}&start=${t.week_start}`}>{t.status === "submitted" ? "Review" : "Open"}</Link></td>
                </tr>
              );
            })}
          </Table>
        )}
      </Card>
    </>
  );
}
