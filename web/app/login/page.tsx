"use client";

import { useRouter } from "next/navigation";
import { FormEvent, ReactNode, useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { Button, Field, inputCls, Notice } from "@/components/ui";
import { RecoveryCodes } from "@/components/recovery-codes";

type Step = "password" | "code" | "setup" | "saved";
type Setup = { key: string; uri: string; qr_svg: string; issuer: string };

export default function LoginPage() {
  const router = useRouter();
  const [step, setStep] = useState<Step>("password");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [useRecovery, setUseRecovery] = useState(false);
  const [setup, setSetup] = useState<Setup | null>(null);
  const [codes, setCodes] = useState<string[]>([]);
  const [savedTick, setSavedTick] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  // A reload in the middle of signing in continues at the same step.
  useEffect(() => {
    api<{ stage: "code" | "setup"; email: string }>("/auth/mfa")
      .then((s) => {
        setEmail(s.email);
        setStep(s.stage);
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (step !== "setup" || setup) return;
    api<Setup>("/auth/mfa/setup", { body: {} })
      .then(setSetup)
      .catch((e) => fail(e));
  }, [step, setup]);

  function fail(err: unknown) {
    if (err instanceof ApiError && err.status === 401) {
      setStep("password");
      setSetup(null);
      setCode("");
    }
    setError(err instanceof ApiError ? err.message : "Could not sign in.");
    setBusy(false);
  }

  async function run(e: FormEvent, fn: () => Promise<void>) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (err) {
      fail(err);
    }
  }

  const signIn = (e: FormEvent) =>
    run(e, async () => {
      const r = await api<{ next: "code" | "setup" }>("/auth/login", { body: { email, password } });
      setPassword("");
      setStep(r.next);
      setBusy(false);
    });

  const verify = (e: FormEvent) =>
    run(e, async () => {
      await api("/auth/mfa/verify", { body: { code } });
      router.replace("/dashboard");
    });

  const confirmSetup = (e: FormEvent) =>
    run(e, async () => {
      const r = await api<{ recovery_codes: string[] }>("/auth/mfa/setup/confirm", { body: { code } });
      setCodes(r.recovery_codes);
      setCode("");
      setStep("saved");
      setBusy(false);
    });

  async function startOver() {
    await api("/auth/logout", { body: {} }).catch(() => {});
    setStep("password");
    setSetup(null);
    setCode("");
    setError("");
  }

  const codeInput = (
    <Field label={useRecovery ? "Recovery code" : "6-digit code"}>
      <input
        className={inputCls + " h-12 text-center font-mono text-lg tracking-[0.3em]"}
        inputMode={useRecovery ? "text" : "numeric"}
        autoComplete="one-time-code"
        autoFocus
        required
        maxLength={useRecovery ? 14 : 7}
        placeholder={useRecovery ? "abcde-fghjk" : "123456"}
        value={code}
        onChange={(e) => setCode(e.target.value)}
      />
    </Field>
  );

  return (
    <div className="grid min-h-screen lg:grid-cols-[1fr_1.1fr]">
      <section className="hidden flex-col justify-between bg-ink p-12 text-white lg:flex">
        <div className="flex items-center gap-2.5">
          <span className="grid h-9 w-9 place-items-center rounded-lg bg-brand font-display font-bold">HR</span>
          <span className="font-display text-xl font-bold">Workspace</span>
        </div>
        <div className="flex max-w-md flex-col gap-6">
          <h1 className="font-display text-4xl font-bold leading-tight">One sign-in for your whole workday.</h1>
          <ul className="flex flex-col gap-4 text-[15px] leading-relaxed text-[#c9ccd3]">
            <li><b className="text-white">Everyone</b> checks in, takes breaks, asks for WFH and fills timesheets.</li>
            <li><b className="text-white">Managers</b> also see their team&apos;s attendance and approve its timesheets.</li>
            <li><b className="text-white">HR</b> approves WFH and corrections and sees the whole company.</li>
            <li><b className="text-white">Admins</b> also manage people, teams, offices and attendance rules.</li>
          </ul>
        </div>
        <p className="text-xs text-[#8a909c]">Signing in needs your password and a code from an authenticator app on your phone.</p>
      </section>
      <section className="flex items-center justify-center px-4 py-12">
        <div className="flex w-full max-w-sm flex-col gap-5">
          {step === "password" && (
            <form onSubmit={signIn} className="flex flex-col gap-5">
              <Heading title="Sign in" sub="Use your work email." />
              {error && <Notice tone="bad">{error}</Notice>}
              <Field label="Work email">
                <input className={inputCls + " h-12"} type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
              </Field>
              <Field label="Password">
                <input className={inputCls + " h-12"} type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
              </Field>
              <Button type="submit" variant="primary" size="lg" disabled={busy}>{busy ? "Signing in…" : "Continue"}</Button>
              <p className="text-xs leading-relaxed text-muted">Forgot your password? Ask an admin to set a new one.</p>
            </form>
          )}

          {step === "code" && (
            <form onSubmit={verify} className="flex flex-col gap-5">
              <Heading
                title="Enter your code"
                sub={useRecovery ? "Type one of the recovery codes you saved. Each one works once." : "Open your authenticator app and type the 6-digit code for HR Workspace."}
              />
              {error && <Notice tone="bad">{error}</Notice>}
              {codeInput}
              <Button type="submit" variant="primary" size="lg" disabled={busy}>{busy ? "Checking…" : "Sign in"}</Button>
              <div className="flex flex-col gap-2 text-sm">
                <button type="button" className="self-start font-semibold text-brand" onClick={() => { setUseRecovery(!useRecovery); setCode(""); setError(""); }}>
                  {useRecovery ? "Use the code from my app" : "Lost your phone? Use a recovery code"}
                </button>
                <button type="button" className="self-start text-muted hover:text-ink" onClick={startOver}>Sign in as someone else</button>
              </div>
              <p className="text-xs leading-relaxed text-muted">No phone and no recovery codes? Ask an admin to reset your authenticator.</p>
            </form>
          )}

          {step === "setup" && (
            <form onSubmit={confirmSetup} className="flex flex-col gap-5">
              <Heading title="Set up your authenticator" sub="Everyone signs in with a code from their phone. This takes a minute, once." />
              {error && <Notice tone="bad">{error}</Notice>}
              <Numbered n={1}>
                Install an authenticator app on your phone: <b>Google Authenticator</b> or <b>Microsoft Authenticator</b> (free, from the App Store or Play Store).
              </Numbered>
              <Numbered n={2}>
                In the app, add an account and scan this code.
                {setup ? (
                  <div className="mt-3 flex flex-col items-center gap-3">
                    {/* eslint-disable-next-line @next/next/no-img-element */}
                    <img src={`data:image/svg+xml;utf8,${encodeURIComponent(setup.qr_svg)}`} alt="QR code to scan with your authenticator app" width={196} height={196} className="rounded-lg border border-line" />
                    <details className="w-full text-[13px]">
                      <summary className="cursor-pointer font-semibold text-brand">Can&apos;t scan? Type a key instead</summary>
                      <p className="mt-2 text-muted">Choose &quot;Enter a setup key&quot;, name it {setup.issuer}, pick &quot;Time based&quot; and type:</p>
                      <code className="mt-1 block break-all rounded-lg bg-canvas px-3 py-2 font-mono text-[13px]">{setup.key}</code>
                      <a href={setup.uri} className="mt-2 inline-block font-semibold text-brand">On this phone? Open the app directly</a>
                    </details>
                  </div>
                ) : (
                  <p className="mt-3 text-muted">Making your code…</p>
                )}
              </Numbered>
              <Numbered n={3}>Type the 6-digit code the app now shows.</Numbered>
              {codeInput}
              <Button type="submit" variant="primary" size="lg" disabled={busy || !setup}>{busy ? "Checking…" : "Turn on and continue"}</Button>
              <button type="button" className="self-start text-sm text-muted hover:text-ink" onClick={startOver}>Cancel</button>
            </form>
          )}

          {step === "saved" && (
            <div className="flex flex-col gap-5">
              <Heading title="Save your recovery codes" sub="If you lose your phone, each of these codes lets you sign in once. Keep them somewhere safe, not only on your phone." />
              <RecoveryCodes codes={codes} email={email} />
              <Notice tone="warn">You will not see these codes again. You can make new ones later from Sign-in security.</Notice>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" className="h-4 w-4" checked={savedTick} onChange={(e) => setSavedTick(e.target.checked)} />
                I have saved my recovery codes
              </label>
              <Button variant="primary" size="lg" disabled={!savedTick} onClick={() => router.replace("/dashboard")}>Continue</Button>
            </div>
          )}
        </div>
      </section>
    </div>
  );
}

function Heading({ title, sub }: { title: string; sub: string }) {
  return (
    <div className="flex flex-col gap-1">
      <h2 className="font-display text-[28px] font-bold">{title}</h2>
      <p className="text-sm leading-relaxed text-muted">{sub}</p>
    </div>
  );
}

function Numbered({ n, children }: { n: number; children: ReactNode }) {
  return (
    <div className="flex gap-3 text-sm leading-relaxed">
      <span className="grid h-6 w-6 shrink-0 place-items-center rounded-full bg-brand-soft text-xs font-bold text-brand-ink">{n}</span>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}
