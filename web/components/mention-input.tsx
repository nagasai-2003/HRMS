"use client";

import { forwardRef, KeyboardEvent, useId, useImperativeHandle, useLayoutEffect, useRef, useState } from "react";
import { PersonRef } from "@/lib/chat";
import { avatarColor, initials } from "@/lib/tasks";
import { cx } from "./ui";

export type Mentionable = PersonRef & { title?: string };

type Props = {
  value: string;
  onChange: (text: string) => void;
  picked: PersonRef[];
  onPick: (picked: PersonRef[]) => void;
  people: Mentionable[];
  /** Enter sends (Shift+Enter is a new line). */
  onSubmit?: () => void;
  onEscape?: () => void;
  onTyping?: () => void;
  placeholder?: string;
  label: string;
  disabled?: boolean;
  minRows?: number;
  maxLength?: number;
  className?: string;
};

export type MentionInputHandle = { focus: () => void; insert: (text: string) => void };

/** A text box where typing @ suggests people to mention. */
export const MentionInput = forwardRef<MentionInputHandle, Props>(function MentionInput(
  { value, onChange, picked, onPick, people, onSubmit, onEscape, onTyping, placeholder, label, disabled, minRows = 2, maxLength = 4000, className },
  ref,
) {
  const box = useRef<HTMLTextAreaElement>(null);
  const listId = useId();
  const [query, setQuery] = useState<{ text: string; start: number } | null>(null);
  const [active, setActive] = useState(0);
  // Where the caret goes after we change the text ourselves. Set in the same
  // render as the new text, so fast typing never lands in the wrong place.
  const caret = useRef<number | null>(null);
  useLayoutEffect(() => {
    const el = box.current;
    if (el && caret.current !== null) {
      el.focus();
      el.setSelectionRange(caret.current, caret.current);
      caret.current = null;
    }
  }, [value]);

  useImperativeHandle(ref, () => ({
    focus: () => box.current?.focus(),
    insert: (text: string) => {
      const el = box.current;
      const at = el?.selectionStart ?? value.length;
      const before = value.slice(0, at);
      const pad = before && !/\s$/.test(before) ? " " : "";
      const next = before + pad + text + " " + value.slice(at);
      caret.current = (before + pad + text + " ").length;
      onChange(next);
    },
  }));

  const matches = query
    ? people
        .filter((p) => {
          const q = query.text.toLowerCase();
          return !q || p.name.toLowerCase().split(/\s+/).some((w) => w.startsWith(q)) || p.name.toLowerCase().startsWith(q);
        })
        .slice(0, 6)
    : [];
  const open = query !== null && matches.length > 0;

  function look(text: string, caret: number) {
    const m = /(^|\s)@([^\s@<>]{0,30})$/.exec(text.slice(0, caret));
    if (m) {
      setQuery({ text: m[2], start: caret - m[2].length - 1 });
      setActive(0);
    } else {
      setQuery(null);
    }
  }

  function choose(p: Mentionable) {
    if (!query) return;
    const at = box.current?.selectionStart ?? value.length;
    const ins = "@" + p.name + " ";
    const next = value.slice(0, query.start) + ins + value.slice(at);
    caret.current = query.start + ins.length;
    onChange(next.slice(0, maxLength));
    if (!picked.some((x) => x.id === p.id)) onPick([...picked, { id: p.id, name: p.name }]);
    setQuery(null);
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (open) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActive((a) => (a + 1) % matches.length);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setActive((a) => (a - 1 + matches.length) % matches.length);
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        choose(matches[active]);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setQuery(null);
        return;
      }
    }
    if (e.key === "Escape" && onEscape) {
      e.preventDefault();
      onEscape();
      return;
    }
    if (e.key === "Enter" && !e.shiftKey && onSubmit && !e.nativeEvent.isComposing) {
      e.preventDefault();
      onSubmit();
    }
  }

  const rows = Math.min(8, Math.max(minRows, value.split("\n").length));

  return (
    <div className="relative">
      {open && (
        <ul id={listId} role="listbox" aria-label="People to mention" className="absolute bottom-full left-0 z-20 mb-1.5 w-72 max-w-full overflow-hidden rounded-xl border border-line bg-white py-1 shadow-lg">
          {matches.map((p, i) => (
            <li
              key={p.id}
              id={`${listId}-${i}`}
              role="option"
              aria-selected={i === active}
              onMouseDown={(e) => {
                e.preventDefault();
                choose(p);
              }}
              onMouseEnter={() => setActive(i)}
              className={cx("flex cursor-pointer items-center gap-2.5 px-3 py-2 text-sm", i === active && "bg-brand-soft")}
            >
              <span className="grid h-7 w-7 shrink-0 place-items-center rounded-full text-[10px] font-bold text-white" style={{ background: avatarColor(p.id) }}>
                {initials(p.name)}
              </span>
              <span className="flex min-w-0 flex-col">
                <span className="truncate font-semibold">{p.name}</span>
                {p.title && <span className="truncate text-xs text-muted">{p.title}</span>}
              </span>
            </li>
          ))}
        </ul>
      )}
      <textarea
        ref={box}
        aria-label={label}
        role="combobox"
        aria-autocomplete="list"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        aria-activedescendant={open ? `${listId}-${active}` : undefined}
        rows={rows}
        maxLength={maxLength}
        disabled={disabled}
        placeholder={placeholder}
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
          look(e.target.value, e.target.selectionStart ?? e.target.value.length);
          onTyping?.();
        }}
        onKeyDown={onKeyDown}
        onClick={(e) => look(value, e.currentTarget.selectionStart ?? value.length)}
        onBlur={() => setTimeout(() => setQuery(null), 120)}
        className={cx(
          "block w-full min-w-0 resize-none rounded-[10px] border border-line-2 bg-white px-3 py-2.5 text-sm leading-relaxed text-ink outline-none focus:border-brand disabled:bg-canvas",
          className,
        )}
      />
    </div>
  );
});
