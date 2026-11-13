import { avatarColor, initials } from "@/lib/tasks";

export function Avatars({ people, size = 26 }: { people: { id: string; name: string }[]; size?: number }) {
  return (
    <span className="flex">
      {people.slice(0, 4).map((p) => (
        <span
          key={p.id}
          title={p.name}
          className="-mr-1.5 grid place-items-center rounded-full border-2 border-white text-[10px] font-bold text-white"
          style={{ width: size, height: size, background: avatarColor(p.id) }}
        >
          {initials(p.name)}
        </span>
      ))}
      {people.length > 4 && <span className="ml-2.5 text-xs text-muted">+{people.length - 4}</span>}
    </span>
  );
}

export function CheckIcon() {
  return (
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M4 4h16v16H4z M8 12l3 3 5-6" />
    </svg>
  );
}

export function CommentIcon() {
  return (
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12z" />
    </svg>
  );
}
