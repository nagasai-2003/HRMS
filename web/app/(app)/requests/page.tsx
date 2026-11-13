"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { dayLabel, kindLabel, weekdays } from "@/lib/format";
import { Badge, Button, Card, cx, Empty, Field, inputCls, Notice, PageHeader, statusTone, Table } from "@/components/ui";

type Req = { id: string; kind: string; day: string; reason: string; fix_in: string; fix_out: string; status: string; is_extra: boolean; decision_note: string; created_at: string };
type Data = {
  requests: Req[];
  today: string;
  month: string;
  wfh: { approved: number; pending: number; quota: number };
  policy: "office" | "hybrid" | "remote";
  remote_weekdays: number[];
  holidays: { day: string; name: string }[];
};

const policyText = (d: Data) =>
  d.policy === "remote" ? "Fully remote" : d.policy === "hybrid" ? `Hybrid · remote on ${d.remote_weekdays.map((w) => weekdays[w - 1]).join(", ")}` : "Office";

export default function RequestsPage() {
  const [data, setData] = useState<Data | null>(null);
  const [month, setMonth] = useState("");
  const [kind, setKind] = useState("wfh");
  const [day, setDay] = useState("");
  const [reason, setReason] = useState("");
  const [fixIn, setFixIn] = useState("09:30");
  const [fixOut, setFixOut] = useState("18:30");
  const [error, setError] = useState("");
  const [ok, setOk] = useState("");
  const [busy, setBusy] = useState(false);

  const load = useCallback(async (m?: string) => {
    const d = await api<Data>(`/requests${m ? `?month=${m}` : ""}`);
    setData(d);
    setMonth(d.month);
    setDay((v) => v || d.today);
  }, []);

  useEffect(() => {
    load().catch((e) => setError(e.message));
  }, [load]);

  if (!data) return error ? <Notice tone="bad">{error}</Notice> : <p className="text-sm text-muted">Loading…</p>;

  const used = data.wfh.approved + data.wfh.pending;
  const dayMonth = day.slice(0, 7);
  const willBeExtra = kind === "wfh" && dayMonth === data.month && used >= data.wfh.quota;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    setOk("");
    try {
      const r = await api<Req>("/requests", { body: { kind, day, reason, fix_in: kind === "correction" ? fixIn : "", fix_out: kind === "correction" ? fixOut : "" } });
      setOk(`${kindLabel[r.kind]} for ${dayLabel(r.day)} sent to HR${r.is_extra ? " as an extra WFH day" : ""}.`);
      setReason("");
      await load(month);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function cancel(id: string) {
    setError("");
    try {
      await api(`/requests/${id}/cancel`, { body: {} });
      await load(month);
    } catch (err) {
      setError((err as Error).message);
    }
  }

  // Calendar for the selected month, weeks starting Monday.
  const [y, m] = month.split("-").map(Number);
  const first = new Date(Date.UTC(y, m - 1, 1));
  const daysIn = new Date(Date.UTC(y, m, 0)).getUTCDate();
  const lead = (first.getUTCDay() + 6) % 7;
  const byDay: Record<string, Req[]> = {};
  for (const r of data.requests) if (r.status !== "cancelled" && r.status !== "rejected") (byDay[r.day] ??= []).push(r);
  const hol = Object.fromEntries(data.holidays.map((h) => [h.day, h.name]));
  const shiftMonth = (n: number) => {
    const d = new Date(Date.UTC(y, m - 1 + n, 1));
    load(d.toISOString().slice(0, 7));
  };

  return (
    <>
      <PageHeader eyebrow={`Work policy: ${policyText(data)}`} title="WFH and requests" />
      {error && <Notice tone="bad">{error}</Notice>}
      {ok && <Notice tone="ok">{ok}</Notice>}

      <div className="grid gap-3 sm:grid-cols-3">
        <div className="flex flex-col gap-1 rounded-2xl border border-line bg-white p-4">
          <span className="text-[13px] text-muted">WFH this month</span>
          <span className="font-display text-[26px] font-bold">{data.wfh.approved} of {data.wfh.quota} used</span>
          <span className="text-xs text-muted">{data.wfh.pending} waiting for approval</span>
        </div>
        <div className="flex flex-col gap-1 rounded-2xl border border-line bg-white p-4">
          <span className="text-[13px] text-muted">Over the limit?</span>
          <span className="text-sm leading-relaxed">You can still ask. It is marked <b>Extra</b> and HR or Admin decide.</span>
        </div>
        <div className="flex flex-col gap-1 rounded-2xl border border-line bg-white p-4">
          <span className="text-[13px] text-muted">Who approves</span>
          <span className="text-sm leading-relaxed">HR or Admin. Your manager can see your requests.</span>
        </div>
      </div>

      <div className="grid gap-5 lg:grid-cols-[1fr_1.2fr]">
        <Card title="New request">
          <form onSubmit={submit} className="flex flex-col gap-4">
            <Field label="Type">
              <select className={inputCls} value={kind} onChange={(e) => setKind(e.target.value)}>
                <option value="wfh">Work from home</option>
                <option value="on_duty">On-duty (client site, travel)</option>
                <option value="correction">Attendance correction</option>
              </select>
            </Field>
            <Field label="Day">
              <input className={inputCls} type="date" required value={day} onChange={(e) => setDay(e.target.value)} />
            </Field>
            {kind === "correction" && (
              <div className="grid grid-cols-2 gap-3">
                <Field label="Correct check-in"><input className={inputCls} type="time" required value={fixIn} onChange={(e) => setFixIn(e.target.value)} /></Field>
                <Field label="Correct check-out"><input className={inputCls} type="time" required value={fixOut} onChange={(e) => setFixOut(e.target.value)} /></Field>
              </div>
            )}
            <Field label="Reason">
              <textarea className={inputCls + " h-24 py-2"} required maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
            </Field>
            {willBeExtra && <Notice tone="warn">You have used your {data.wfh.quota} WFH days this month. This one will be marked Extra.</Notice>}
            {kind === "on_duty" && <p className="text-xs text-muted">On an approved on-duty day you can check in from anywhere. Your location is still recorded.</p>}
            {kind === "correction" && <p className="text-xs text-muted">When approved, these times replace that day&apos;s check-in and check-out.</p>}
            <Button type="submit" variant="primary" disabled={busy}>{busy ? "Sending…" : "Send request"}</Button>
          </form>
        </Card>

        <Card
          title={first.toLocaleDateString("en-GB", { month: "long", year: "numeric", timeZone: "UTC" })}
          aside={
            <span className="flex gap-1">
              <Button size="sm" onClick={() => shiftMonth(-1)} aria-label="Previous month">←</Button>
              <Button size="sm" onClick={() => shiftMonth(1)} aria-label="Next month">→</Button>
            </span>
          }
        >
          <div className="grid grid-cols-7 gap-1 text-center text-xs">
            {weekdays.map((w) => <span key={w} className="py-1 font-semibold text-muted">{w}</span>)}
            {Array.from({ length: lead }).map((_, i) => <span key={`b${i}`} />)}
            {Array.from({ length: daysIn }).map((_, i) => {
              const d = `${month}-${String(i + 1).padStart(2, "0")}`;
              const wd = ((lead + i) % 7) + 1;
              const reqs = byDay[d] ?? [];
              const remote = data.policy === "remote" || (data.policy === "hybrid" && data.remote_weekdays.includes(wd));
              const wfh = reqs.find((r) => r.kind === "wfh" || r.kind === "on_duty");
              return (
                <button
                  key={d}
                  type="button"
                  onClick={() => setDay(d)}
                  title={hol[d] ?? reqs.map((r) => `${kindLabel[r.kind]} (${r.status})`).join(", ")}
                  className={cx(
                    "flex min-h-14 flex-col items-center gap-0.5 rounded-lg border p-1",
                    d === day ? "border-brand" : "border-transparent",
                    d === data.today && "font-bold",
                    hol[d] ? "bg-canvas text-muted" : wfh?.status === "approved" ? "bg-ok-soft" : wfh ? "bg-warn-soft" : remote ? "bg-violet-soft" : "hover:bg-canvas",
                  )}
                >
                  <span>{i + 1}</span>
                  <span className="text-[10px] leading-tight text-muted">
                    {hol[d] ? "Holiday" : wfh ? (wfh.kind === "on_duty" ? "On-duty" : wfh.is_extra ? "WFH extra" : "WFH") + (wfh.status === "pending" ? "?" : "") : remote ? "Remote" : ""}
                  </span>
                </button>
              );
            })}
          </div>
          <div className="mt-3 flex flex-wrap gap-3 text-xs text-muted">
            <span className="flex items-center gap-1"><span className="h-3 w-3 rounded bg-ok-soft" /> WFH approved</span>
            <span className="flex items-center gap-1"><span className="h-3 w-3 rounded bg-warn-soft" /> Waiting</span>
            <span className="flex items-center gap-1"><span className="h-3 w-3 rounded bg-violet-soft" /> Your remote day</span>
          </div>
        </Card>
      </div>

      <Card title="My requests">
        {data.requests.length === 0 ? (
          <Empty>No requests yet.</Empty>
        ) : (
          <Table head={["Day", "Type", "Reason", "Status", ""]} minWidth={680}>
            {data.requests.map((r) => (
              <tr key={r.id}>
                <td className="whitespace-nowrap font-semibold">{dayLabel(r.day)}</td>
                <td>
                  <span className="flex flex-wrap items-center gap-1.5">
                    {kindLabel[r.kind]}
                    {r.is_extra && <Badge tone="warn">Extra</Badge>}
                    {r.kind === "correction" && <span className="text-xs text-muted">{r.fix_in}–{r.fix_out}</span>}
                  </span>
                </td>
                <td className="max-w-72 text-ink-2">
                  {r.reason}
                  {r.decision_note && <div className="text-xs text-muted">Note: {r.decision_note}</div>}
                </td>
                <td><Badge tone={statusTone(r.status)}>{r.status[0].toUpperCase() + r.status.slice(1)}</Badge></td>
                <td className="text-right">
                  {(r.status === "pending" || (r.status === "approved" && r.kind !== "correction" && r.day >= data.today)) && (
                    <Button size="sm" variant="ghost" onClick={() => cancel(r.id)}>Cancel</Button>
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
