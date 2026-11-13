"use client";

import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { dayLabel, kindLabel } from "@/lib/format";
import { RoleGate, useMe } from "@/components/shell";
import { Badge, Button, Card, Empty, inputCls, Notice, PageHeader, statusTone, Table, Tabs } from "@/components/ui";

type Req = {
  id: string; user_name: string; kind: string; day: string; reason: string; fix_in: string; fix_out: string; status: string;
  is_extra: boolean; decided_by_name: string; decision_note: string; wfh_used: number; can_decide: boolean;
};

export default function ApprovalsPage() {
  return (
    <RoleGate allow={(m) => m.can.team_views}>
      <Inner />
    </RoleGate>
  );
}

function Inner() {
  const me = useMe();
  const [status, setStatus] = useState<"pending" | "approved" | "rejected" | "all">("pending");
  const [kind, setKind] = useState("all");
  const [data, setData] = useState<{ requests: Req[]; quota: number } | null>(null);
  const [notes, setNotes] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");

  const load = useCallback(async () => {
    setData(await api(`/approvals?status=${status}`));
  }, [status]);
  useEffect(() => {
    load().catch((e) => setError(e.message));
  }, [load]);

  async function decide(id: string, approve: boolean) {
    setBusy(id);
    setError("");
    try {
      await api(`/approvals/${id}/decide`, { body: { approve, note: notes[id] ?? "" } });
      await load();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy("");
    }
  }

  const rows = (data?.requests ?? []).filter((r) => kind === "all" || (kind === "extra" ? r.is_extra : r.kind === kind));
  const count = (k: string) => (data?.requests ?? []).filter((r) => (k === "extra" ? r.is_extra : r.kind === k)).length;

  return (
    <>
      <PageHeader eyebrow={me.can.decide_requests ? "WFH, on-duty and corrections. You decide." : "Your team's requests. HR or Admin decide them."} title="Request approvals" />
      {error && <Notice tone="bad">{error}</Notice>}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Tabs value={status} onChange={setStatus} items={[{ value: "pending", label: "Waiting" }, { value: "approved", label: "Approved" }, { value: "rejected", label: "Rejected" }, { value: "all", label: "All" }]} />
        <Tabs
          value={kind}
          onChange={setKind}
          items={[
            { value: "all", label: `All ${data?.requests.length ?? ""}` },
            { value: "wfh", label: `WFH ${count("wfh")}` },
            { value: "extra", label: `Extra WFH ${count("extra")}` },
            { value: "on_duty", label: `On-duty ${count("on_duty")}` },
            { value: "correction", label: `Corrections ${count("correction")}` },
          ]}
        />
      </div>
      <Card>
        {!data ? <p className="text-sm text-muted">Loading…</p> : rows.length === 0 ? <Empty>Nothing here.</Empty> : (
          <Table head={["Person", "Day", "Request", "Reason", "WFH used", me.can.decide_requests ? "Decision" : "Status"]} minWidth={980}>
            {rows.map((r) => (
              <tr key={r.id}>
                <td className="font-semibold">{r.user_name}</td>
                <td className="whitespace-nowrap">{dayLabel(r.day)}</td>
                <td>
                  <span className="flex flex-wrap items-center gap-1.5">{kindLabel[r.kind]}{r.is_extra && <Badge tone="warn">Extra</Badge>}</span>
                  {r.kind === "correction" && <span className="text-xs text-muted">Set to {r.fix_in} – {r.fix_out}</span>}
                </td>
                <td className="max-w-64 text-ink-2">{r.reason}</td>
                <td>{r.kind === "wfh" ? `${r.wfh_used} of ${data.quota}` : "–"}</td>
                <td>
                  {r.can_decide ? (
                    <div className="flex flex-col gap-2">
                      <input className={inputCls + " h-9"} placeholder="Note (needed to reject)" aria-label={`Note for ${r.user_name}`} value={notes[r.id] ?? ""} onChange={(e) => setNotes({ ...notes, [r.id]: e.target.value })} />
                      <div className="flex gap-2">
                        <Button size="sm" variant="primary" disabled={busy === r.id} onClick={() => decide(r.id, true)}>Approve</Button>
                        <Button size="sm" variant="danger" disabled={busy === r.id || !(notes[r.id] ?? "").trim()} onClick={() => decide(r.id, false)}>Reject</Button>
                      </div>
                    </div>
                  ) : (
                    <div className="flex flex-col gap-1">
                      <Badge tone={statusTone(r.status)}>{r.status[0].toUpperCase() + r.status.slice(1)}</Badge>
                      {r.decided_by_name && <span className="text-xs text-muted">by {r.decided_by_name}{r.decision_note ? `: ${r.decision_note}` : ""}</span>}
                    </div>
                  )}
                </td>
              </tr>
            ))}
          </Table>
        )}
      </Card>
    </>
  );
}
