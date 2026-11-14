"use client";

import { Fragment } from "react";
import { PersonRef, pieces } from "@/lib/chat";
import { cx } from "./ui";

/** Shows chat or comment text: mentions, links and task keys stand out. */
export function RichText({ body, mentions, me, className }: { body: string; mentions: PersonRef[]; me?: string; className?: string }) {
  return (
    <span className={cx("whitespace-pre-wrap break-words", className)}>
      {pieces(body, mentions).map((p, i) => {
        switch (p.kind) {
          case "mention":
            return (
              <span key={i} className={cx("rounded px-1 font-semibold", p.id === me ? "bg-warn-soft text-warn" : "bg-brand-soft text-brand-ink")}>
                @{p.name}
              </span>
            );
          case "link":
            return (
              <a key={i} href={p.href} target="_blank" rel="noopener noreferrer nofollow" className="text-brand underline underline-offset-2 hover:text-brand-ink">
                {p.text}
              </a>
            );
          case "task":
            return (
              <span key={i} className="font-semibold text-ink">
                {p.key}
              </span>
            );
          default:
            return <Fragment key={i}>{p.text}</Fragment>;
        }
      })}
    </span>
  );
}
