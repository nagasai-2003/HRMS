"use client";

import { FormEvent, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { useMe } from "@/components/shell";
import { Badge, Button, Card, Field, inputCls, Notice, PageHeader } from "@/components/ui";
import { RecoveryCodes } from "@/components/recovery-codes";

type Security = { mfa_enabled_at: string | null; recovery_left: number };

export default function SecurityPage() {
  const me = useMe();
  const [data, setData] = useState<Security | null>(null);
  const [code, setCode] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () => api<Security>("/me/security").then(setData).catch((e) => setError(e.message));
  useEffect(() => {
    load();
  }, []);

  async function newCodes(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const r = await api<{ recovery_codes: string[] }>("/me/security/recovery-codes", { body: { code } });
      setCodes(r.recovery_codes);
      setCode("");
      load();
    } catch (err) {
      setError((err as Error).message);
    }
    setBusy(false);
  }

  return (
    <>
      <PageHeader eyebrow="Your account" title="Sign-in security" />
      {error && <Notice tone="bad">{error}</Notice>}
      <div className="grid gap-5 lg:grid-cols-2">
        <Card title="Authenticator app" aside={data?.mfa_enabled_at ? <Badge tone="ok">On</Badge> : null}>
          <div className="flex flex-col gap-2 text-sm leading-relaxed">
            <p>You sign in with your password and a 6-digit code from the authenticator app on your phone.</p>
            {data?.mfa_enabled_at && <p className="text-muted">Turned on {new Date(data.mfa_enabled_at).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })}.</p>}
            <p className="text-muted">
              New phone or lost phone? Use a recovery code to sign in, or ask an admin to reset your authenticator. You then set it up again at your next sign-in.
              {me.is_owner && " As the Owner, no other Admin can reset yours. If you lose both your phone and your recovery codes, whoever runs the server has to reset it there, so keep the codes safe."}
            </p>
          </div>
        </Card>

        <Card title="Recovery codes" aside={data ? `${data.recovery_left} of 10 left` : null}>
          {codes ? (
            <div className="flex flex-col gap-3">
              <Notice tone="warn">Your old codes no longer work. Save these now: you will not see them again.</Notice>
              <RecoveryCodes codes={codes} email={me.email} />
              <Button size="sm" className="self-start" onClick={() => setCodes(null)}>Done</Button>
            </div>
          ) : (
            <form onSubmit={newCodes} className="flex flex-col gap-3">
              <p className="text-sm leading-relaxed text-muted">
                Each recovery code lets you sign in once without your phone.
                {data && data.recovery_left <= 3 && <b className="text-warn"> You are running low: make new ones.</b>}
              </p>
              <div className="flex flex-wrap items-end gap-2">
                <Field label="Code from your app">
                  <input className={inputCls + " w-40 font-mono tracking-widest"} inputMode="numeric" autoComplete="one-time-code" required maxLength={7} placeholder="123456" value={code} onChange={(e) => setCode(e.target.value)} />
                </Field>
                <Button type="submit" variant="primary" disabled={busy}>Make new recovery codes</Button>
              </div>
            </form>
          )}
        </Card>
      </div>
    </>
  );
}
