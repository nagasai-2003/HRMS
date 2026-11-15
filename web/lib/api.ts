// Thin wrapper over fetch for the Go API. Errors carry the server's message.

export class ApiError extends Error {
  constructor(public status: number, message: string, public body: Record<string, unknown> = {}) {
    super(message);
  }
}

export async function api<T = unknown>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  const method = init.method ?? (init.body === undefined ? "GET" : "POST");
  const res = await fetch(`/api${path}`, {
    method,
    // The API only accepts JSON writes (its CSRF guard), DELETE included.
    headers: method === "GET" ? undefined : { "Content-Type": "application/json" },
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
    credentials: "same-origin",
    cache: "no-store",
  });
  const text = await res.text();
  let data: Record<string, unknown> = {};
  try {
    data = text ? JSON.parse(text) : {};
  } catch {
    data = { error: text };
  }
  if (!res.ok) {
    if (res.status === 401 && typeof window !== "undefined" && !path.startsWith("/auth/")) {
      window.location.href = "/login";
    }
    throw new ApiError(res.status, String(data.error ?? `Request failed (${res.status})`), data);
  }
  return data as T;
}

export type Role = "employee" | "manager" | "hr" | "admin";

export type Me = {
  id: string;
  name: string;
  email: string;
  title: string;
  role: Role;
  /** The one Admin other Admins cannot change. */
  is_owner: boolean;
  mfa_enabled_at: string | null;
  work_policy: "office" | "hybrid" | "remote";
  remote_weekdays: number[];
  office: { id: string; name: string } | null;
  teams: { id: string; name: string; key: string; is_manager: boolean }[];
  can: { team_views: boolean; decide_requests: boolean; review_timesheets: boolean; admin: boolean; holidays: boolean };
};
