"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { clock, dayLabel, hm, modeLabel } from "@/lib/format";
import { Badge, Button, Card, cx, Empty, inputCls, Notice, PageHeader } from "@/components/ui";

type Summary = {
  phase: "out" | "in" | "break" | "done";
  mode: string;
  first_in: string | null;
  last_out: string | null;
  worked_min: number;
  break_min: number;
  breaks: { reason: string; start: string; end: string | null; minutes: number }[];
  auto_closed: boolean;
};
type Today = {
  day: string;
  mode: string;
  holiday: string;
  remote: boolean;
  summary: Summary;
  late: boolean;
  events: { id: string; kind: string; at: string; reason: string; note: string; office?: string; distance_m?: number; ip: string; auto: boolean; mode: string }[];
  blocked: { at: string; action: string; code: string; message: string }[];
  break_reasons: string[];
  rule: "both" | "either";
  max_accuracy_m: number;
  late_after: string;
  auto_checkout: string;
  ip: string;
  network_ok: boolean;
};
type Check = {
  allowed: boolean;
  code: string;
  message: string;
  in_fence: boolean;
  accuracy_ok: boolean;
  network_ok: boolean;
  fence_check: boolean;
  network_check: boolean;
  office?: string;
  distance_m?: number;
  radius_m?: number;
};
type Fix = { lat: number; lng: number; accuracy_m: number; at: number };
type DayRow = { day: string; mode: string; holiday: string; summary: Summary; late: boolean };

const kindText: Record<string, string> = { check_in: "Checked in", break_start: "Break started", break_end: "Break ended", check_out: "Checked out" };
const actionText: Record<string, string> = { check_in: "Check in", break_start: "Start break", break_end: "End break", check_out: "Check out" };

// Read the browser location. Rejects with a readable message.
function locate(): Promise<Fix> {
  return new Promise((resolve, reject) => {
    if (!("geolocation" in navigator)) return reject(new Error("This browser cannot share location."));
    navigator.geolocation.getCurrentPosition(
      (p) => resolve({ lat: p.coords.latitude, lng: p.coords.longitude, accuracy_m: p.coords.accuracy, at: Date.now() }),
      (e) => reject(new Error(e.code === e.PERMISSION_DENIED ? "Location is blocked for this site. Allow it in the browser's site settings." : "Could not read your location. Try again near a window or with Wi-Fi on.")),
      { enableHighAccuracy: true, timeout: 15000, maximumAge: 0 },
    );
  });
}

const devMode = process.env.NODE_ENV !== "production";

export default function AttendancePage() {
  const [today, setToday] = useState<Today | null>(null);
  const [week, setWeek] = useState<DayRow[]>([]);
  const [fix, setFix] = useState<Fix | null>(null);
  const [fixError, setFixError] = useState("");
  const [check, setCheck] = useState<Check | null>(null);
  const [busy, setBusy] = useState<string>("");
  const [blocked, setBlocked] = useState<(Check & { action: string }) | null>(null);
  const [error, setError] = useState("");
  const [breakOpen, setBreakOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [note, setNote] = useState("");
  const [devFix, setDevFix] = useState<"" | "inside" | "outside">("");
  const [, tick] = useState(0);
  const devRef = useRef(devFix);
  devRef.current = devFix;

  const load = useCallback(async () => {
    const [t, h] = await Promise.all([api<Today>("/attendance/today"), api<{ days: DayRow[] }>("/attendance/history")]);
    setToday(t);
    setWeek(h.days.slice(0, 7));
  }, []);

  const getFix = useCallback(async (): Promise<Fix | null> => {
    if (devRef.current === "inside") return { lat: 17.4486, lng: 78.3917, accuracy_m: 20, at: Date.now() };
    if (devRef.current === "outside") return { lat: 17.465, lng: 78.3915, accuracy_m: 20, at: Date.now() };
    try {
      const f = await locate();
      setFixError("");
      return f;
    } catch (e) {
      setFixError((e as Error).message);
      return null;
    }
  }, []);

  const refreshCheck = useCallback(async () => {
    const f = await getFix();
    setFix(f);
    const body = f ? { lat: f.lat, lng: f.lng, accuracy_m: f.accuracy_m } : {};
    setCheck(await api<Check>("/attendance/check", { body }).catch(() => null));
  }, [getFix]);

  useEffect(() => {
    load().catch((e) => setError(e.message));
    refreshCheck();
  }, [load, refreshCheck]);

  useEffect(() => {
    refreshCheck();
  }, [devFix, refreshCheck]);

  // Keep the running clocks moving.
  useEffect(() => {
    const t = setInterval(() => tick((n) => n + 1), 30000);
    return () => clearInterval(t);
  }, []);

  async function act(action: string) {
    setBusy(action);
    setError("");
    setBlocked(null);
    try {
      const f = await getFix();
      setFix(f);
      const body: Record<string, unknown> = { action, ...(f ? { lat: f.lat, lng: f.lng, accuracy_m: f.accuracy_m } : {}) };
      if (action === "break_start") Object.assign(body, { reason, note });
      const t = await api<Today>("/attendance/action", { body });
      setToday(t);
      setBreakOpen(false);
      setReason("");
      setNote("");
      api<{ days: DayRow[] }>("/attendance/history").then((h) => setWeek(h.days.slice(0, 7)));
      refreshCheck();
    } catch (e) {
      if (e instanceof ApiError && e.status === 403 && "allowed" in e.body) {
        setBlocked({ ...(e.body as unknown as Check), action });
        setCheck(e.body as unknown as Check);
        load();
      } else {
        setError((e as Error).message);
      }
    } finally {
      setBusy("");
    }
  }

  if (!today) {
    return error ? <Notice tone="bad">{error}</Notice> : <p className="text-sm text-muted">Loading…</p>;
  }

  const s = today.summary;
  const phaseBadge = {
    out: <Badge>Not checked in</Badge>,
    in: <Badge tone="ok">Working</Badge>,
    break: <Badge tone="warn">On break · {s.breaks.at(-1)?.reason}</Badge>,
    done: <Badge tone="info">Checked out</Badge>,
  }[s.phase];

  const actions: { action: string; variant: "primary" | "secondary" }[] =
    s.phase === "out" || s.phase === "done"
      ? [{ action: "check_in", variant: "primary" }]
      : s.phase === "in"
        ? [{ action: "break_start", variant: "secondary" }, { action: "check_out", variant: "primary" }]
        : [{ action: "break_end", variant: "primary" }, { action: "check_out", variant: "secondary" }];

  return (
    <>
      <PageHeader
        eyebrow={`${dayLabel(today.day, { weekday: "long", day: "numeric", month: "long" })} · ${modeLabel[today.mode] ?? today.mode}${today.holiday ? ` · ${today.holiday}` : ""}`}
        title="Attendance"
      />

      {error && <Notice tone="bad">{error}</Notice>}
      {blocked && (
        <Notice tone="bad">
          <b>{actionText[blocked.action] ?? "That"} was blocked.</b> {blocked.message} This attempt is logged for HR.
        </Notice>
      )}

      <div className="grid gap-5 lg:grid-cols-[1.4fr_1fr]">
        <Card>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-col gap-1">
              <span className="text-[13px] text-muted">Today</span>
              <div className="flex items-center gap-2">{phaseBadge}{today.late && <Badge tone="warn">Late (after {today.late_after})</Badge>}</div>
            </div>
            <div className="flex gap-6 text-right">
              <div>
                <div className="text-xs text-muted">Checked in</div>
                <div className="font-display text-2xl font-bold">{clock(s.first_in)}</div>
              </div>
              <div>
                <div className="text-xs text-muted">Worked</div>
                <div className="font-display text-2xl font-bold">{hm(s.worked_min)}</div>
              </div>
              <div>
                <div className="text-xs text-muted">Breaks</div>
                <div className="font-display text-2xl font-bold">{hm(s.break_min)}</div>
              </div>
            </div>
          </div>

          <div className="mt-5 flex flex-wrap gap-3">
            {actions.map((a) => (
              <Button
                key={a.action}
                size="lg"
                variant={a.variant}
                className="min-w-40"
                disabled={!!busy || today.mode === "holiday"}
                onClick={() => (a.action === "break_start" ? setBreakOpen((o) => !o) : act(a.action))}
                aria-expanded={a.action === "break_start" ? breakOpen : undefined}
              >
                {busy === a.action ? "Checking…" : s.phase === "done" && a.action === "check_in" ? "Check in again" : actionText[a.action]}
              </Button>
            ))}
          </div>

          {breakOpen && (
            <div className="mt-4 flex flex-col gap-3 rounded-xl border border-line bg-canvas p-4">
              <span className="text-sm font-semibold">Why are you taking a break?</span>
              <div className="flex flex-wrap gap-2" role="radiogroup" aria-label="Break reason">
                {today.break_reasons.map((r) => (
                  <button
                    key={r}
                    type="button"
                    role="radio"
                    aria-checked={reason === r}
                    onClick={() => setReason(r)}
                    className={cx("min-h-9 rounded-full border px-3.5 text-[13px] font-semibold", reason === r ? "border-brand bg-brand text-white" : "border-line-2 bg-white text-ink-2")}
                  >
                    {r}
                  </button>
                ))}
              </div>
              <input className={inputCls} placeholder={reason === "Other" ? "Note (needed for Other)" : "Note (optional)"} value={note} maxLength={300} onChange={(e) => setNote(e.target.value)} aria-label="Break note" />
              <div className="flex gap-2">
                <Button variant="primary" disabled={!reason || (reason === "Other" && !note.trim()) || !!busy} onClick={() => act("break_start")}>
                  {busy === "break_start" ? "Checking…" : "Start break"}
                </Button>
                <Button variant="ghost" onClick={() => setBreakOpen(false)}>Cancel</Button>
              </div>
            </div>
          )}

          {s.auto_closed && <p className="mt-4 text-sm text-warn">Yesterday&apos;s check-out was added by the system. Ask for a correction if the time is wrong.</p>}
          {today.mode === "holiday" && <p className="mt-4 text-sm text-muted">Today is a holiday. Ask HR for on-duty if you need to work.</p>}

          <h3 className="mt-6 mb-2 text-sm font-semibold">Today&apos;s log</h3>
          {today.events.length === 0 ? (
            <p className="text-sm text-muted">Nothing yet.</p>
          ) : (
            <ol className="flex flex-col">
              {today.events.map((e) => (
                <li key={e.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-[#eef0f3] py-2.5 text-sm">
                  <span className="w-12 font-display font-bold">{clock(e.at)}</span>
                  <span className="font-semibold">{kindText[e.kind]}</span>
                  {e.reason && <Badge tone="warn">{e.reason}</Badge>}
                  <span className="text-muted">
                    {e.office ? `${e.office}${e.distance_m != null ? ` · ${e.distance_m} m` : ""}` : modeLabel[e.mode] ?? e.mode}
                    {e.ip && ` · ${e.ip}`}
                    {e.auto && " · added by the system"}
                  </span>
                  {e.note && <span className="w-full pl-15 text-xs text-muted">{e.note}</span>}
                </li>
              ))}
            </ol>
          )}
        </Card>

        <div className="flex flex-col gap-5">
          <Card title="Checks before you press" aside={<button type="button" className="font-semibold text-brand" onClick={refreshCheck}>Check again</button>}>
            <ul className="flex flex-col gap-3 text-sm">
              <CheckRow
                ok={!!fix}
                label="Location shared"
                detail={fix ? `Accuracy about ${Math.round(fix.accuracy_m)} m` : fixError || "Waiting for the browser…"}
              />
              {today.remote ? (
                <CheckRow ok label="Remote day" detail={`${modeLabel[today.mode]}: location is recorded, the office network is not needed.`} />
              ) : (
                <>
                  <CheckRow
                    ok={!!check?.in_fence}
                    label="Inside an office"
                    detail={
                      !check || !fix
                        ? "Needs your location."
                        : check.office
                          ? `${check.distance_m} m from ${check.office} (allowed ${check.radius_m} m)${!check.accuracy_ok ? ` · reading too vague (over ${today.max_accuracy_m} m)` : ""}`
                          : check.message
                    }
                  />
                  <CheckRow ok={today.network_ok} label="Office network" detail={`Your network: ${today.ip}${today.network_ok ? "" : " (not an office network)"}`} />
                  <li className="text-xs text-muted">Rule: {today.rule === "both" ? "you need both the location and the office network." : "either the location or the office network is enough."}</li>
                </>
              )}
            </ul>
            {check && (
              <div className="mt-4">
                {check.allowed ? <Notice tone="ok">You can check in, take breaks and check out from here.</Notice> : <Notice tone="warn">{check.message}</Notice>}
              </div>
            )}
            {devMode && (
              <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-dashed border-line-2 pt-3 text-xs text-muted">
                <span>Development only, test location:</span>
                {(["", "inside", "outside"] as const).map((v) => (
                  <button key={v} type="button" onClick={() => setDevFix(v)} className={cx("rounded-full border px-2.5 py-1 font-semibold", devFix === v ? "border-ink bg-ink text-white" : "border-line-2 bg-white")}>
                    {v === "" ? "Real" : v === "inside" ? "Inside HQ" : "1.9 km away"}
                  </button>
                ))}
              </div>
            )}
          </Card>

          {today.blocked.length > 0 && (
            <Card title="Blocked today" aside="HR can see these">
              <ul className="flex flex-col gap-2 text-sm">
                {today.blocked.map((b, i) => (
                  <li key={i} className="flex gap-3 border-t border-[#eef0f3] pt-2 first:border-0 first:pt-0">
                    <span className="w-12 font-display font-bold">{clock(b.at)}</span>
                    <span className="flex-1 text-bad">{b.message}</span>
                  </li>
                ))}
              </ul>
            </Card>
          )}
        </div>
      </div>

      <Card title="Last 7 days" aside={`Days left open are closed by the system at ${today.auto_checkout}`}>
        {week.length === 0 ? (
          <Empty>No days yet.</Empty>
        ) : (
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-7">
            {week.map((d) => (
              <div key={d.day} className={cx("flex flex-col gap-1 rounded-xl border p-3 text-sm", d.day === today.day ? "border-brand" : "border-line")}>
                <span className="text-xs font-semibold text-muted">{dayLabel(d.day)}</span>
                {d.summary.first_in ? (
                  <>
                    <span className="font-semibold">
                      {clock(d.summary.first_in)} – {d.summary.phase === "done" ? clock(d.summary.last_out) : "now"}
                    </span>
                    <span className="text-xs text-muted">{hm(d.summary.worked_min)} worked</span>
                    <div className="flex flex-wrap gap-1">
                      {d.late && <Badge tone="warn">Late</Badge>}
                      {d.summary.auto_closed && <Badge tone="bad">No check-out</Badge>}
                      {d.mode !== "office" && <Badge tone="violet">{modeLabel[d.mode]}</Badge>}
                    </div>
                  </>
                ) : (
                  <span className="text-xs text-muted">{d.holiday || (d.mode !== "office" ? modeLabel[d.mode] : "No attendance")}</span>
                )}
              </div>
            ))}
          </div>
        )}
      </Card>
    </>
  );
}

function CheckRow({ ok, label, detail }: { ok: boolean; label: string; detail: string }) {
  return (
    <li className="flex gap-3">
      <span aria-hidden className={cx("mt-0.5 grid h-5 w-5 shrink-0 place-items-center rounded-full text-xs font-bold text-white", ok ? "bg-[#15803d]" : "bg-[#b42318]")}>
        {ok ? "✓" : "!"}
      </span>
      <div className="flex flex-col">
        <span className="font-semibold">
          {label} <span className="sr-only">{ok ? "passed" : "not passed"}</span>
        </span>
        <span className="text-[13px] text-muted">{detail}</span>
      </div>
    </li>
  );
}
