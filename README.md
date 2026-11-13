# HRMS

The company HR app: attendance with location and network checks, WFH and other requests, weekly timesheets, and team views for managers, HR and admins.

Frames (design): https://claude.ai/artifact/852jUKSLz5XPTCVwP7yNT6

## Stack

| Part | Choice |
|---|---|
| API | Go 1.27, chi, pgx + sqlc, golang-migrate (migrations run on start) |
| Web | Next.js 16 (App Router), TypeScript, Tailwind 4, Recharts |
| Data | PostgreSQL 16, Redis 7 (login rate limits now; chat and presence later) |
| Later steps | WebSockets for chat, LiveKit for calls, River job queue, SMTP mail, MinIO/S3 for files |
| Run | docker compose locally, Caddy on a VPS in production |

## Run it

```bash
make db      # Postgres on :5442, Redis on :6390
make api     # API on :8180 (creates the tables)
make seed    # demo company, once
make web     # http://localhost:3100
```

Or `make up` to run everything in Docker.

### Demo accounts

All of them use `SEED_PASSWORD` from `.env`.

| Email | Access |
|---|---|
| admin@company.test | Admin (Rahul Verma) |
| hr@company.test | HR (Priya Menon) |
| manager@company.test | Manager of Frontend (Sneha N) |
| employee@company.test | Employee in Frontend, hybrid with Wednesdays remote (Ananya Rao) |

More people are in `backend/cmd/seed/main.go`.

In development, the office network list includes this computer (`127.0.0.0/8`, `::1`), and the Attendance page has "test location" buttons. **Remove both local networks before going live.** The buttons don't appear in a production build.

## Who sees what

| | Employee | Manager | HR | Admin |
|---|---|---|---|---|
| Own attendance, requests, timesheets, dashboard | ✓ | ✓ | ✓ | ✓ |
| Team attendance, people, dashboards | | own teams | everyone | everyone |
| Approve timesheets | | own teams | everyone | everyone |
| See WFH/on-duty/correction requests | | own teams (read only) | ✓ decide | ✓ decide |
| Holidays and timesheet projects | | | ✓ | ✓ |
| People, teams, managers, offices, networks, rules | | | | ✓ |

- A manager is someone ticked as "Manages this team" on Teams and people. Ticking it gives an employee the Manager role.
- Nobody can decide their own request or timesheet.

## Attendance rules

Every check-in, break start, break end and check-out goes through `backend/internal/rules/verify.go` in this order:

1. **Holiday.** Blocked, unless on-duty or WFH is approved for that day.
2. **Remote day.** Approved WFH, approved on-duty, a hybrid person's remote weekday, or a fully remote person. Location is required and recorded; the office checks are skipped.
3. **Location shared.** If not, the action is blocked.
4. **Accuracy and geofence.** The reading must be at least as accurate as the limit (100 m by default) and inside the nearest office's radius.
5. **Office network.** The request's public IP must be in an allowed IP or range.
6. **Both / either.** The admin chooses whether 4 and 5 must both pass or just one.

Every refused attempt is logged and shown to the person, their manager and HR.

- **Times:** come from the server clock.
- **IP address:** taken from the connection. `X-Forwarded-For` is read only from proxies listed in `TRUSTED_PROXIES`.
- **Forgotten check-out:** days left open are closed at the auto check-out time (23:59 by default) and flagged.

### WFH

- The monthly quota is 2 by default and set on Attendance rules.
- Every request needs HR or Admin approval.
- Once approved and pending requests reach the quota, new requests are marked **Extra**.
- A hybrid remote day needs no request.

## Timesheets

- One sheet per person per week: rows of day, project, task reference, hours and note.
- Each day shows logged time next to the time from attendance (check-in to check-out, minus breaks).
- A day is flagged when logged time is over 30 minutes more than time at work, or when time is logged on a day with no attendance.
- Flow: draft, then submitted (can be recalled), then approved or sent back with a note. A sent-back sheet becomes editable again.
- Managers approve their teams; HR and Admin approve anyone.

## Layout

```
backend/
  cmd/api, cmd/seed
  migrations/            SQL, embedded and applied on start
  queries/               sqlc queries → internal/store
  internal/rules         check-in decisions, day totals (unit tested)
  internal/clientip      trusted-proxy IP parsing (unit tested)
  internal/api           HTTP handlers
web/app/(app)/           pages: dashboard, attendance, requests, timesheets, team/*, approvals, admin/*
infra/docker-compose.yml
```

## Production notes

- Put Caddy in front. Send `/api/*` straight to the API and everything else to the web app. Set `TRUSTED_PROXIES` to Caddy's address only.
- Set `APP_ENV=production` for Secure cookies.
- Replace the example office IPs with your real static IPs.

## Next steps

1. Tasks with team-private boards and custom status groups; link timesheet rows to tasks.
2. Chat (WebSockets), calls (LiveKit), mail.
3. Company performance dashboard for Admin.
4. Integration tests, email alerts via River.
