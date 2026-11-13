export function hm(min: number | null | undefined): string {
  if (min == null || min <= 0) return "0m";
  const h = Math.floor(min / 60);
  const m = min % 60;
  return h ? `${h}h ${String(m).padStart(2, "0")}m` : `${m}m`;
}

export function hours(min: number): string {
  return (min / 60).toFixed(min % 60 === 0 ? 0 : 1);
}

const tz = "Asia/Kolkata";

export function clock(iso: string | null | undefined): string {
  if (!iso) return "–";
  return new Date(iso).toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", timeZone: tz });
}

export function dayLabel(ymd: string, opts: Intl.DateTimeFormatOptions = { weekday: "short", day: "numeric", month: "short" }): string {
  const [y, m, d] = ymd.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString("en-GB", { ...opts, timeZone: "UTC" });
}

export function addDays(ymd: string, n: number): string {
  const [y, m, d] = ymd.split("-").map(Number);
  const t = new Date(Date.UTC(y, m - 1, d + n));
  return t.toISOString().slice(0, 10);
}

export const modeLabel: Record<string, string> = {
  office: "Office",
  wfh: "WFH (approved)",
  on_duty: "On-duty",
  hybrid_remote: "Hybrid remote",
  remote: "Remote",
  holiday: "Holiday",
  correction: "Corrected",
};

export const kindLabel: Record<string, string> = { wfh: "Work from home", on_duty: "On-duty", correction: "Attendance correction" };

export const weekdays = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
