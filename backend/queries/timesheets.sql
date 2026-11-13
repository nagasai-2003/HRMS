-- name: EnsureTimesheet :one
INSERT INTO timesheets (user_id, week_start) VALUES (@user_id, @week_start)
ON CONFLICT (user_id, week_start) DO UPDATE SET user_id = excluded.user_id
RETURNING *;

-- name: FindTimesheet :one
SELECT * FROM timesheets WHERE user_id = @user_id AND week_start = @week_start;

-- name: GetTimesheet :one
SELECT * FROM timesheets WHERE id = @id;

-- name: ListEntries :many
SELECT e.*, p.code AS project_code, p.name AS project_name
FROM timesheet_entries e JOIN projects p ON p.id = e.project_id
WHERE e.timesheet_id = @timesheet_id
ORDER BY e.sort, e.day;

-- name: DeleteEntries :exec
DELETE FROM timesheet_entries WHERE timesheet_id = @timesheet_id;

-- name: InsertEntry :exec
INSERT INTO timesheet_entries (timesheet_id, day, project_id, task_ref, minutes, note, sort)
VALUES (@timesheet_id, @day, @project_id, @task_ref, @minutes, @note, @sort);

-- Editing a rejected sheet turns it back into a draft.
-- name: TouchTimesheet :exec
UPDATE timesheets SET updated_at = now(),
    status = CASE WHEN status = 'rejected' THEN 'draft' ELSE status END
WHERE id = @id;

-- name: SubmitTimesheet :one
UPDATE timesheets SET status = 'submitted', submitted_at = now(), decided_by = NULL, decided_at = NULL, updated_at = now()
WHERE id = @id AND status IN ('draft', 'rejected')
RETURNING *;

-- name: RecallTimesheet :one
UPDATE timesheets SET status = 'draft', submitted_at = NULL, updated_at = now()
WHERE id = @id AND status = 'submitted'
RETURNING *;

-- name: DecideTimesheet :one
UPDATE timesheets SET status = @status, decided_by = @decided_by, decided_at = now(),
    decision_note = @decision_note, updated_at = now()
WHERE id = @id AND status = 'submitted'
RETURNING *;

-- name: ListTimesheetsForReview :many
SELECT t.*, u.name AS user_name,
    coalesce((SELECT sum(minutes) FROM timesheet_entries e WHERE e.timesheet_id = t.id), 0)::int AS total_minutes
FROM timesheets t JOIN users u ON u.id = t.user_id
WHERE (sqlc.narg('status')::text IS NULL OR t.status = sqlc.narg('status')::text)
  AND (sqlc.narg('user_ids')::uuid[] IS NULL OR t.user_id = ANY(sqlc.narg('user_ids')::uuid[]))
  AND t.status <> 'draft'
ORDER BY (t.status = 'submitted') DESC, t.week_start DESC, u.name
LIMIT 300;

-- name: ListMyTimesheets :many
SELECT t.*,
    coalesce((SELECT sum(minutes) FROM timesheet_entries e WHERE e.timesheet_id = t.id), 0)::int AS total_minutes
FROM timesheets t
WHERE t.user_id = @user_id
ORDER BY t.week_start DESC
LIMIT 26;
