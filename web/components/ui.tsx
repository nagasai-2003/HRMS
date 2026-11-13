"use client";

import { ReactNode } from "react";

export function cx(...c: (string | false | null | undefined)[]) {
  return c.filter(Boolean).join(" ");
}

export function PageHeader({ eyebrow, title, actions }: { eyebrow?: ReactNode; title: string; actions?: ReactNode }) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-4">
      <div className="flex flex-col gap-1">
        {eyebrow && <span className="text-[13px] text-muted">{eyebrow}</span>}
        <h1 className="font-display text-[28px] font-bold leading-tight">{title}</h1>
      </div>
      {actions && <div className="flex flex-wrap items-end gap-2.5">{actions}</div>}
    </header>
  );
}

export function Card({ children, className, title, aside }: { children: ReactNode; className?: string; title?: ReactNode; aside?: ReactNode }) {
  return (
    <section className={cx("min-w-0 rounded-2xl border border-line bg-white p-5", className)}>
      {(title || aside) && (
        <div className="mb-3.5 flex flex-wrap items-baseline justify-between gap-2">
          {title && <h2 className="text-base font-semibold">{title}</h2>}
          {aside && <div className="text-[13px] text-muted">{aside}</div>}
        </div>
      )}
      {children}
    </section>
  );
}

type BtnProps = React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" | "danger" | "ghost"; size?: "md" | "lg" | "sm" };

export function Button({ variant = "secondary", size = "md", className, ...rest }: BtnProps) {
  return (
    <button
      type="button"
      {...rest}
      className={cx(
        "inline-flex items-center justify-center gap-2 rounded-[10px] font-semibold transition-colors disabled:cursor-not-allowed disabled:opacity-50",
        size === "lg" && "min-h-12 px-5 text-[15px]",
        size === "md" && "min-h-10 px-4 text-sm",
        size === "sm" && "min-h-8 px-3 text-[13px]",
        variant === "primary" && "bg-brand text-white hover:bg-brand-ink",
        variant === "secondary" && "border border-line-2 bg-white text-ink hover:bg-canvas",
        variant === "danger" && "border border-[#f3c2bd] bg-white text-bad hover:bg-bad-soft",
        variant === "ghost" && "text-brand hover:bg-brand-soft",
        className,
      )}
    />
  );
}

const tones = {
  ok: "bg-ok-soft text-ok",
  warn: "bg-warn-soft text-warn",
  bad: "bg-bad-soft text-bad",
  info: "bg-brand-soft text-brand-ink",
  violet: "bg-violet-soft text-violet",
  neutral: "bg-canvas text-ink-2",
};
export type Tone = keyof typeof tones;

export function Badge({ tone = "neutral", children }: { tone?: Tone; children: ReactNode }) {
  return <span className={cx("inline-flex items-center whitespace-nowrap rounded-full px-2.5 py-1 text-xs font-semibold", tones[tone])}>{children}</span>;
}

export function Stat({ label, value, note }: { label: string; value: ReactNode; note?: ReactNode }) {
  return (
    <div className="flex flex-col gap-1 rounded-2xl border border-line bg-white p-4">
      <span className="text-[13px] text-muted">{label}</span>
      <span className="font-display text-[26px] font-bold leading-tight">{value}</span>
      {note && <span className="text-xs text-muted">{note}</span>}
    </div>
  );
}

export function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: ReactNode }) {
  return (
    <label className="flex flex-col gap-1.5 text-[13px] font-semibold text-ink-2">
      {label}
      {children}
      {hint && <span className="text-xs font-normal text-muted">{hint}</span>}
    </label>
  );
}

export const inputCls = "h-10 w-full min-w-0 rounded-[10px] border border-line-2 bg-white px-3 text-sm font-normal text-ink outline-none focus:border-brand";

export function Notice({ tone = "info", children }: { tone?: "info" | "ok" | "warn" | "bad"; children: ReactNode }) {
  const c = {
    info: "border-[#c9d6f7] bg-brand-soft text-brand-ink",
    ok: "border-[#b7dfc3] bg-ok-soft text-ok",
    warn: "border-[#f5d7a8] bg-warn-soft text-warn",
    bad: "border-[#f3c2bd] bg-bad-soft text-bad",
  }[tone];
  return <div className={cx("rounded-xl border px-4 py-3 text-sm leading-relaxed", c)} role={tone === "bad" ? "alert" : "status"}>{children}</div>;
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="py-6 text-center text-sm text-muted">{children}</p>;
}

export function Table({ head, children, minWidth = 720 }: { head: ReactNode[]; children: ReactNode; minWidth?: number }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-sm" style={{ minWidth }}>
        <thead>
          <tr className="text-left text-xs text-muted">
            {head.map((h, i) => (
              <th key={i} className="px-3 py-2.5 font-semibold">{h}</th>
            ))}
          </tr>
        </thead>
        <tbody className="[&>tr]:border-t [&>tr]:border-[#eef0f3] [&_td]:px-3 [&_td]:py-3 [&_td]:align-middle">{children}</tbody>
      </table>
    </div>
  );
}

export function Tabs<T extends string>({ value, onChange, items }: { value: T; onChange: (v: T) => void; items: { value: T; label: ReactNode }[] }) {
  return (
    <div className="flex flex-wrap gap-1.5" role="tablist">
      {items.map((it) => (
        <button
          key={it.value}
          type="button"
          role="tab"
          aria-selected={value === it.value}
          onClick={() => onChange(it.value)}
          className={cx(
            "min-h-9 rounded-full px-3.5 text-[13px] font-semibold",
            value === it.value ? "bg-ink text-white" : "border border-line-2 bg-white text-ink-2 hover:bg-canvas",
          )}
        >
          {it.label}
        </button>
      ))}
    </div>
  );
}

export function statusTone(s: string): Tone {
  return ({ approved: "ok", submitted: "info", pending: "warn", rejected: "bad", cancelled: "neutral", draft: "neutral" } as Record<string, Tone>)[s] ?? "neutral";
}
