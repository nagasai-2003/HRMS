"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { RoleGate } from "@/components/shell";
import { Badge, Button, Card, cx, Field, inputCls, Notice, PageHeader, Table } from "@/components/ui";

type Settings = { rule: "both" | "either"; max_accuracy_m: number; shift_start: string; late_after: string; auto_checkout: string; wfh_quota: number; timezone: string };
type Office = { id: string; name: string; lat: number; lng: number; radius_m: number; active: boolean };
type Network = { id: string; cidr: string; label: string };
type Data = { settings: Settings; offices: Office[]; networks: Network[]; your_ip: string };

export default function RulesPage() {
  return (
    <RoleGate allow={(m) => m.can.admin}>
      <Inner />
    </RoleGate>
  );
}

function Inner() {
  const [data, setData] = useState<Data | null>(null);
  const [s, setS] = useState<Settings | null>(null);
  const [office, setOffice] = useState({ id: "", name: "", lat: "", lng: "", radius_m: "150" });
  const [net, setNet] = useState({ cidr: "", label: "" });
  const [error, setError] = useState("");
  const [ok, setOk] = useState("");

  const load = useCallback(async () => {
    const d = await api<Data>("/admin/rules");
    setData(d);
    setS(d.settings);
  }, []);
  useEffect(() => {
    load().catch((e) => setError(e.message));
  }, [load]);

  async function run(fn: () => Promise<unknown>, done: string) {
    setError("");
    setOk("");
    try {
      await fn();
      await load();
      setOk(done);
      return true;
    } catch (e) {
      setError((e as Error).message);
      return false;
    }
  }

  if (!data || !s) return error ? <Notice tone="bad">{error}</Notice> : <p className="text-sm text-muted">Loading…</p>;

  function useMyLocation() {
    navigator.geolocation.getCurrentPosition(
      (p) => setOffice((o) => ({ ...o, lat: p.coords.latitude.toFixed(6), lng: p.coords.longitude.toFixed(6) })),
      () => setError("Could not read your location. Type the coordinates instead."),
      { enableHighAccuracy: true, timeout: 15000 },
    );
  }

  async function saveOffice(e: FormEvent) {
    e.preventDefault();
    const body = { name: office.name, lat: Number(office.lat), lng: Number(office.lng), radius_m: Number(office.radius_m), active: true };
    if (await run(() => (office.id ? api(`/admin/offices/${office.id}`, { method: "PATCH", body }) : api("/admin/offices", { body })), `${office.name} saved.`)) {
      setOffice({ id: "", name: "", lat: "", lng: "", radius_m: "150" });
    }
  }

  return (
    <>
      <PageHeader eyebrow="What check-in, break and check-out must pass on office days" title="Attendance rules" />
      {error && <Notice tone="bad">{error}</Notice>}
      {ok && <Notice tone="ok">{ok}</Notice>}

      <Card title="Rules">
        <form
          className="flex flex-col gap-5"
          onSubmit={(e) => { e.preventDefault(); run(() => api("/admin/rules", { method: "PUT", body: s }), "Rules saved. They apply from the next action."); }}
        >
          <fieldset className="flex flex-col gap-2">
            <legend className="mb-1 text-[13px] font-semibold text-ink-2">On office days, a person must be</legend>
            {([["both", "Inside an office AND on an office network", "Safest. A VPN or a phone hotspot will not pass."], ["either", "Inside an office OR on an office network", "Easier when the office Wi-Fi is patchy or GPS is weak indoors."]] as const).map(([v, label, hint]) => (
              <label key={v} className={cx("flex cursor-pointer gap-3 rounded-xl border-2 p-3", s.rule === v ? "border-brand bg-brand-soft" : "border-line")}>
                <input type="radio" name="rule" className="mt-1" checked={s.rule === v} onChange={() => setS({ ...s, rule: v })} />
                <span className="flex flex-col"><span className="text-sm font-semibold">{label}</span><span className="text-xs text-muted">{hint}</span></span>
              </label>
            ))}
            <p className="text-xs text-muted">Approved WFH, on-duty, hybrid remote days and fully remote people skip these checks. Their location is still recorded.</p>
          </fieldset>
          <div className="grid gap-4 sm:grid-cols-3 lg:grid-cols-6">
            <Field label="Location accuracy (m)" hint="Vaguer readings do not count as inside"><input className={inputCls} type="number" min={10} max={2000} value={s.max_accuracy_m} onChange={(e) => setS({ ...s, max_accuracy_m: Number(e.target.value) })} /></Field>
            <Field label="Shift starts"><input className={inputCls} type="time" value={s.shift_start} onChange={(e) => setS({ ...s, shift_start: e.target.value })} /></Field>
            <Field label="Late after"><input className={inputCls} type="time" value={s.late_after} onChange={(e) => setS({ ...s, late_after: e.target.value })} /></Field>
            <Field label="Auto check-out at" hint="For days left open"><input className={inputCls} type="time" value={s.auto_checkout} onChange={(e) => setS({ ...s, auto_checkout: e.target.value })} /></Field>
            <Field label="WFH days a month" hint="More are marked Extra"><input className={inputCls} type="number" min={0} max={31} value={s.wfh_quota} onChange={(e) => setS({ ...s, wfh_quota: Number(e.target.value) })} /></Field>
            <Field label="Time zone"><input className={inputCls} value={s.timezone} onChange={(e) => setS({ ...s, timezone: e.target.value })} /></Field>
          </div>
          <div><Button type="submit" variant="primary">Save rules</Button></div>
        </form>
      </Card>

      <div className="grid gap-5 lg:grid-cols-2">
        <Card title="Office locations" aside="Geofence: a circle around each office">
          <Table head={["Office", "Centre", "Radius", ""]} minWidth={460}>
            {data.offices.map((o) => (
              <tr key={o.id} className={o.active ? "" : "opacity-60"}>
                <td className="font-semibold">{o.name} {!o.active && <Badge>Off</Badge>}</td>
                <td className="tabular-nums text-ink-2">
                  <a className="text-brand" target="_blank" rel="noreferrer" href={`https://www.openstreetmap.org/?mlat=${o.lat}&mlon=${o.lng}#map=17/${o.lat}/${o.lng}`}>{o.lat.toFixed(4)}, {o.lng.toFixed(4)}</a>
                </td>
                <td>{o.radius_m} m</td>
                <td className="whitespace-nowrap text-right">
                  <Button size="sm" variant="ghost" onClick={() => setOffice({ id: o.id, name: o.name, lat: String(o.lat), lng: String(o.lng), radius_m: String(o.radius_m) })}>Edit</Button>
                  <Button size="sm" variant="ghost" onClick={() => confirm(`Delete ${o.name}?`) && run(() => api(`/admin/offices/${o.id}`, { method: "DELETE" }), `${o.name} deleted.`)}>Delete</Button>
                </td>
              </tr>
            ))}
          </Table>
          <form onSubmit={saveOffice} className="mt-4 grid gap-3 border-t border-line pt-4 sm:grid-cols-2">
            <Field label="Name"><input className={inputCls} required value={office.name} onChange={(e) => setOffice({ ...office, name: e.target.value })} /></Field>
            <Field label="Radius (m)"><input className={inputCls} type="number" min={20} max={5000} required value={office.radius_m} onChange={(e) => setOffice({ ...office, radius_m: e.target.value })} /></Field>
            <Field label="Latitude"><input className={inputCls} inputMode="decimal" required value={office.lat} onChange={(e) => setOffice({ ...office, lat: e.target.value })} /></Field>
            <Field label="Longitude"><input className={inputCls} inputMode="decimal" required value={office.lng} onChange={(e) => setOffice({ ...office, lng: e.target.value })} /></Field>
            <div className="flex flex-wrap gap-2 sm:col-span-2">
              <Button type="submit" variant="primary">{office.id ? "Save office" : "Add office"}</Button>
              <Button onClick={useMyLocation}>Use my location</Button>
              {office.id && <Button variant="ghost" onClick={() => setOffice({ id: "", name: "", lat: "", lng: "", radius_m: "150" })}>Cancel</Button>}
            </div>
            <p className="text-xs text-muted sm:col-span-2">Tip: stand in the middle of the office and press &quot;Use my location&quot;. Keep the radius above 100 m; indoor GPS drifts.</p>
          </form>
        </Card>

        <Card title="Office networks" aside={`Your network now: ${data.your_ip}`}>
          <Table head={["Public IP or range", "Label", ""]} minWidth={420}>
            {data.networks.map((n) => (
              <tr key={n.id}>
                <td className="font-semibold tabular-nums">{n.cidr}</td>
                <td className="text-ink-2">{n.label}</td>
                <td className="text-right"><Button size="sm" variant="ghost" onClick={() => run(() => api(`/admin/networks/${n.id}`, { method: "DELETE" }), `${n.cidr} removed.`)}>Remove</Button></td>
              </tr>
            ))}
          </Table>
          <form className="mt-4 grid gap-3 border-t border-line pt-4 sm:grid-cols-[1fr_1fr_auto]" onSubmit={async (e) => { e.preventDefault(); if (await run(() => api("/admin/networks", { body: net }), `${net.cidr} allowed.`)) setNet({ cidr: "", label: "" }); }}>
            <Field label="IP or range"><input className={inputCls} required placeholder="49.205.112.18 or 183.82.96.0/28" value={net.cidr} onChange={(e) => setNet({ ...net, cidr: e.target.value })} /></Field>
            <Field label="Label"><input className={inputCls} placeholder="HQ fibre" value={net.label} onChange={(e) => setNet({ ...net, label: e.target.value })} /></Field>
            <div className="flex items-end gap-2">
              <Button type="submit" variant="primary">Allow</Button>
            </div>
          </form>
          <div className="mt-3 flex flex-wrap items-center gap-2 text-xs text-muted">
            In the office now? <Button size="sm" onClick={() => setNet({ cidr: data.your_ip, label: "Office Wi-Fi" })}>Use my current network</Button>
          </div>
          <p className="mt-2 text-xs leading-relaxed text-muted">Browsers cannot read the Wi-Fi name, so the office is recognised by its public IP address. Ask your internet provider for a static IP.</p>
        </Card>
      </div>
    </>
  );
}
