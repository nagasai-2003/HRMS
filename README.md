# HRMS

The company HR app: attendance with location and network checks, WFH and other requests, weekly timesheets, private team task boards, chat with @mentions and notifications, and team views for managers, HR and admins.

Frames (design): https://claude.ai/artifact/852jUKSLz5XPTCVwP7yNT6

## Stack

| Part | Choice |
|---|---|
| API | Go 1.27, chi, pgx + sqlc, golang-migrate (migrations run on start) |
| Web | Next.js 16 (App Router), TypeScript, Tailwind 4, Recharts |
| Data | PostgreSQL 16, Redis 7 (login and message rate limits, live events between API instances, online status) |
| Live | One WebSocket per browser tab at `/api/live` (coder/websocket) |
| Later steps | LiveKit for calls, River job queue, SMTP mail, MinIO/S3 for files |
| Run | docker compose locally, Caddy on a VPS in production |

## Run it

```bash
make db      # Postgres on :5442, Redis on :6390
make api     # API on :8180 (creates the tables)
make seed    # demo company, once (includes demo tasks)
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

A database seeded before task boards existed can get the demo tasks with `cd backend && go run ./cmd/seed -demo-tasks` (it only fills empty Frontend and Backend boards). One seeded before chat can get demo channels and messages with `go run ./cmd/seed -demo-chat` (it only fills empty channels).

In development, the office network list includes this computer (`127.0.0.0/8`, `::1`), and the Attendance page has "test location" buttons. **Remove both local networks before going live.** The buttons don't appear in a production build.

## Who sees what

| | Employee | Manager | HR | Admin |
|---|---|---|---|---|
| Own attendance, requests, timesheets, dashboard | ✓ | ✓ | ✓ | ✓ |
| Team attendance, people, dashboards | | own teams | everyone | everyone |
| Approve timesheets | | own teams | everyone | everyone |
| See WFH/on-duty/correction requests | | own teams (read only) | ✓ decide | ✓ decide |
| Holidays and timesheet projects | | | ✓ | ✓ |
| A team's task board: see, add, edit, move, comment | members | members | members | every team |
| Edit a team's status groups; delete any of its tasks | | team managers | | ✓ |
| A team's chat channels: read, post, open a new one | members | members | members | members |
| Rename a channel | its creator | team managers | | |
| Archive a channel; delete others' messages in it | | team managers | | |
| Direct and group messages | anyone | anyone | anyone | anyone |
| People, teams, managers, offices, networks, rules | | | | ✓ |

- A manager is someone ticked as "Manages this team" on Teams and people. Ticking it gives an employee the Manager role.
- Nobody can decide their own request or timesheet.
- Task boards follow team membership, not role: HR sees only the boards of teams HR people are in. Anyone may delete a task they created.
- Chat channels are stricter than boards: only the team's members can read them, Admin included. Direct messages are readable only by the people in them.

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

## Tasks

Every team has a private board (`/tasks`). Only the team's members and Admin can open it; to anyone else its tasks do not exist.

- **Task numbers** use the team's key: FE-142. Admin sets the key on Teams and people (it is suggested from the name). Changing it renames every task; numbers are never reused.
- **Status groups** are the board's columns. Each team names its own, picks a colour and says whether each one counts as *Not started*, *Active* or *Done*. Moving a task into a Done group stamps it completed; moving it out clears that. A board always keeps one Done group, and a group with tasks needs a target group before it is deleted. Templates (Standard, Software, Design, Sales) replace every group, so they only work on an empty board.
- **A task** has a title, description, assignees (team members only), priority, start and due dates, an estimate, labels, subtasks, comments and an activity log.
- **Views:** board (drag cards between and within columns), list (change the status in place), calendar by due date, and *My tasks* across all your teams. Done tasks leave the board 30 days after they finish.
- **Timesheets:** type a task key such as `FE-142` in a timesheet row's Task box. If the task is on one of your boards the row links to it, and the task shows the hours logged against it per person. Deleting a task keeps the hours on the timesheet, unlinked.

## Chat and notifications

- **Team channels.** Every team starts with `#<team>-general`, which can't be archived. Any member can open more (up to 30 a team). Archived channels stay readable but nobody can post.
- **Direct messages.** 1:1 or a group of up to 9 people, across teams. The same people always land in the same conversation.
- **Messages.** Enter sends, Shift+Enter is a new line. You can edit and delete your own; a deleted message shows as "This message was deleted". Typing a task key such as `FE-6` shows a card with its status, but only to people who can open that board. Links open in a new tab. Up to 4,000 characters, and 30 messages in 10 seconds per person.
- **Live.** New messages, edits, deletes, typing, unread counts and notifications arrive without reloading. Green dot = the person has the app open. If the connection drops, the page reconnects and catches up.
- **Unread.** The Chat menu item shows unread messages. A conversation is marked read when you scroll to its newest message. People added to a team start with its history marked read.
- **Search.** The search box filters channels and people. "Search messages" looks through every conversation you can read and jumps to the message.
- **@mentions.** Type `@` in chat or a task comment to pick a person. Only people who can read the conversation (or open the task) can be mentioned.
- **Notifications (the bell).** You get one when someone assigns you a task, mentions you in chat or a task comment, or comments on a task you are assigned to or reported. A short pop-up shows for new ones. Direct messages show as unread counts instead. Notifications older than 90 days are deleted.
- **Not built yet:** threads, reactions, file attachments and browser push notifications.

## Layout

```
backend/
  cmd/api, cmd/seed
  migrations/            SQL, embedded and applied on start
  queries/               sqlc queries → internal/store
  internal/rules         check-in decisions, day totals (unit tested)
  internal/clientip      trusted-proxy IP parsing (unit tested)
  internal/boards        status templates, team keys, task refs, labels (unit tested)
  internal/chattext      mentions, task keys in messages, channel names (unit tested)
  internal/live          WebSocket fan-out over Redis, online status (unit tested)
  internal/api           HTTP handlers
web/app/(app)/           pages: dashboard, chat, attendance, requests, timesheets, tasks/*, team/*, approvals, admin/*
infra/docker-compose.yml
```

## Production notes

- Put Caddy in front. Send `/api/*` straight to the API and everything else to the web app. Set `TRUSTED_PROXIES` to Caddy's address only. Caddy passes WebSockets (`/api/live`) through as is.
- The live socket only accepts pages from its own host. If the web app is served from another host, list it in `ALLOWED_ORIGINS` (for example `hr.example.com`).
- Run more than one API instance only with Redis on: Redis carries live events between them.
- Set `APP_ENV=production` for Secure cookies.
- Replace the example office IPs with your real static IPs.

## Next steps

1. Voice and video calls (LiveKit) from channels and direct messages.
2. Mail inside the company.
3. Performance dashboards: team view for managers, company view for Admin, from task completion, timesheets and attendance.
4. File storage (MinIO/S3) for attachments on tasks, chat and mail.
5. Integration tests in the repo, email alerts via River.
