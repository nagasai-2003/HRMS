"use client";

import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { Button, Field, inputCls, Notice } from "@/components/ui";

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/auth/login", { body: { email, password } });
      router.replace("/dashboard");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Could not sign in.");
      setBusy(false);
    }
  }

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
        <p className="text-xs text-[#8a909c]">Attendance needs your location and the office network on office days.</p>
      </section>
      <section className="flex items-center justify-center px-4 py-12">
        <form onSubmit={submit} className="flex w-full max-w-sm flex-col gap-5">
          <div className="flex flex-col gap-1">
            <h2 className="font-display text-[28px] font-bold">Sign in</h2>
            <p className="text-sm text-muted">Use your work email.</p>
          </div>
          {error && <Notice tone="bad">{error}</Notice>}
          <Field label="Work email">
            <input className={inputCls + " h-12"} type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <Field label="Password">
            <input className={inputCls + " h-12"} type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <Button type="submit" variant="primary" size="lg" disabled={busy}>
            {busy ? "Signing in…" : "Sign in"}
          </Button>
          <p className="text-xs leading-relaxed text-muted">Forgot your password? Ask an admin to set a new one.</p>
        </form>
      </section>
    </div>
  );
}
