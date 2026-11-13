-- Teams whose boards this person can open: their own, or every team for Admin.
-- name: ListTaskTeams :many
SELECT t.id, t.name, t.key,
    coalesce(tm.is_manager, false)::bool AS is_manager,
    (tm.user_id IS NOT NULL)::bool AS is_member,
    (SELECT count(*) FROM tasks k JOIN task_statuses s ON s.id = k.status_id
     WHERE k.team_id = t.id AND s.category <> 'done')::int AS open_tasks
FROM teams t
LEFT JOIN team_members tm ON tm.team_id = t.id AND tm.user_id = @user_id
WHERE @all_teams::bool OR tm.user_id IS NOT NULL
ORDER BY t.name;

-- name: GetTeam :one
SELECT * FROM teams WHERE id = @id;

-- name: GetMembership :one
SELECT is_manager FROM team_members WHERE team_id = @team_id AND user_id = @user_id;

-- ---- status groups ----

-- name: ListStatuses :many
SELECT s.*, (SELECT count(*) FROM tasks k WHERE k.status_id = s.id)::int AS task_count
FROM task_statuses s
WHERE s.team_id = @team_id
ORDER BY s.position, s.created_at;

-- name: GetStatus :one
SELECT * FROM task_statuses WHERE id = @id;

-- name: CreateStatus :one
INSERT INTO task_statuses (team_id, name, color, category, position)
VALUES (@team_id, @name, @color, @category, @position)
RETURNING *;

-- name: UpdateStatus :one
UPDATE task_statuses SET name = @name, color = @color, category = @category
WHERE id = @id
RETURNING *;

-- name: SetStatusPosition :exec
UPDATE task_statuses SET position = @position WHERE id = @id;

-- name: DeleteStatus :exec
DELETE FROM task_statuses WHERE id = @id;

-- name: DeleteTeamStatuses :exec
DELETE FROM task_statuses WHERE team_id = @team_id;

-- Moves every task out of one group (before deleting it).
-- name: MoveAllTasks :exec
UPDATE tasks SET status_id = @to_id, updated_at = now(),
    completed_at = CASE WHEN @to_done::bool THEN coalesce(completed_at, now()) ELSE NULL END
WHERE status_id = @from_id;

-- After a group's category changes, its tasks follow.
-- name: SyncCompleted :exec
UPDATE tasks SET completed_at = CASE WHEN @done::bool THEN coalesce(completed_at, now()) ELSE NULL END
WHERE status_id = @status_id;

-- name: CountTeamTasks :one
SELECT count(*)::int FROM tasks WHERE team_id = @team_id;

-- ---- tasks ----

-- name: NextTaskNumber :one
UPDATE teams SET task_seq = task_seq + 1 WHERE id = @id RETURNING task_seq;

-- name: CreateTask :one
INSERT INTO tasks (team_id, number, title, description, status_id, priority, labels,
    start_date, due_date, estimate_minutes, reporter_id, position, completed_at)
VALUES (@team_id, @number, @title, @description, @status_id, @priority, @labels,
    @start_date, @due_date, @estimate_minutes, @reporter_id,
    coalesce((SELECT max(position) FROM tasks WHERE status_id = @status_id), 0) + 1,
    CASE WHEN @done::bool THEN now() END)
RETURNING *;

-- name: GetTask :one
SELECT k.*, t.key AS team_key, t.name AS team_name,
    s.name AS status_name, s.color AS status_color, s.category AS status_category,
    u.name AS reporter_name
FROM tasks k
JOIN teams t ON t.id = k.team_id
JOIN task_statuses s ON s.id = k.status_id
JOIN users u ON u.id = k.reporter_id
WHERE k.id = @id;

-- Done tasks finished more than 30 days ago drop off the board.
-- name: ListBoardTasks :many
SELECT k.id, k.number, k.title, k.status_id, k.priority, k.labels, k.start_date, k.due_date,
    k.estimate_minutes, k.position, k.completed_at, k.created_at, k.updated_at,
    (SELECT count(*) FROM task_checklist c WHERE c.task_id = k.id)::int AS checklist_total,
    (SELECT count(*) FROM task_checklist c WHERE c.task_id = k.id AND c.done)::int AS checklist_done,
    (SELECT count(*) FROM task_comments c WHERE c.task_id = k.id)::int AS comment_count,
    coalesce((SELECT sum(e.minutes) FROM timesheet_entries e WHERE e.task_id = k.id), 0)::int AS logged_minutes
FROM tasks k
WHERE k.team_id = @team_id
  AND (k.completed_at IS NULL OR k.completed_at > now() - interval '30 days')
ORDER BY k.position, k.number
LIMIT 2000;

-- Open tasks assigned to this person in teams they still belong to,
-- plus ones they finished in the last 14 days.
-- name: ListMyTasks :many
SELECT k.id, k.team_id, t.key AS team_key, t.name AS team_name, k.number, k.title,
    k.status_id, s.name AS status_name, s.color AS status_color, s.category AS status_category,
    k.priority, k.labels, k.start_date, k.due_date, k.estimate_minutes, k.completed_at, k.updated_at,
    coalesce((SELECT sum(e.minutes) FROM timesheet_entries e WHERE e.task_id = k.id), 0)::int AS logged_minutes
FROM task_assignees a
JOIN tasks k ON k.id = a.task_id
JOIN teams t ON t.id = k.team_id
JOIN task_statuses s ON s.id = k.status_id
WHERE a.user_id = @user_id
  AND EXISTS (SELECT 1 FROM team_members tm WHERE tm.team_id = k.team_id AND tm.user_id = @user_id)
  AND (k.completed_at IS NULL OR k.completed_at > now() - interval '14 days')
ORDER BY (s.category = 'done'), k.due_date NULLS LAST, t.key, k.number
LIMIT 500;

-- name: ListAssigneesFor :many
SELECT a.task_id, u.id AS user_id, u.name
FROM task_assignees a JOIN users u ON u.id = a.user_id
WHERE a.task_id = ANY(@task_ids::uuid[])
ORDER BY u.name;

-- name: AddAssignee :exec
INSERT INTO task_assignees (task_id, user_id) VALUES (@task_id, @user_id) ON CONFLICT DO NOTHING;

-- name: RemoveAssignee :exec
DELETE FROM task_assignees WHERE task_id = @task_id AND user_id = @user_id;

-- name: UpdateTask :exec
UPDATE tasks SET title = @title, description = @description, priority = @priority, labels = @labels,
    start_date = @start_date, due_date = @due_date, estimate_minutes = @estimate_minutes, updated_at = now()
WHERE id = @id;

-- name: SetTaskStatus :exec
UPDATE tasks SET status_id = @status_id, updated_at = now(),
    completed_at = CASE WHEN @done::bool THEN coalesce(completed_at, now()) ELSE NULL END
WHERE id = @id;

-- name: ColumnTaskIDs :many
SELECT id FROM tasks WHERE status_id = @status_id AND id <> @exclude_id ORDER BY position, number;

-- name: SetTaskPosition :exec
UPDATE tasks SET position = @position WHERE id = @id;

-- name: TouchTask :exec
UPDATE tasks SET updated_at = now() WHERE id = @id;

-- name: DeleteTask :exec
DELETE FROM tasks WHERE id = @id;

-- ---- checklist, comments, activity ----

-- name: ListChecklist :many
SELECT * FROM task_checklist WHERE task_id = @task_id ORDER BY position, created_at;

-- name: CountChecklist :one
SELECT count(*)::int FROM task_checklist WHERE task_id = @task_id;

-- name: AddChecklistItem :one
INSERT INTO task_checklist (task_id, title, position)
VALUES (@task_id, @title, coalesce((SELECT max(position) + 1 FROM task_checklist WHERE task_id = @task_id), 0))
RETURNING *;

-- name: GetChecklistItem :one
SELECT * FROM task_checklist WHERE id = @id AND task_id = @task_id;

-- name: UpdateChecklistItem :exec
UPDATE task_checklist SET title = @title, done = @done WHERE id = @id;

-- name: DeleteChecklistItem :exec
DELETE FROM task_checklist WHERE id = @id;

-- name: ListComments :many
SELECT c.*, u.name AS author_name
FROM task_comments c JOIN users u ON u.id = c.author_id
WHERE c.task_id = @task_id
ORDER BY c.created_at;

-- name: AddComment :one
INSERT INTO task_comments (task_id, author_id, body) VALUES (@task_id, @author_id, @body) RETURNING *;

-- name: GetComment :one
SELECT * FROM task_comments WHERE id = @id AND task_id = @task_id;

-- name: DeleteComment :exec
DELETE FROM task_comments WHERE id = @id;

-- name: AddActivity :exec
INSERT INTO task_activity (task_id, actor_id, text) VALUES (@task_id, @actor_id, @text);

-- name: ListActivity :many
SELECT a.*, u.name AS actor_name
FROM task_activity a JOIN users u ON u.id = a.actor_id
WHERE a.task_id = @task_id
ORDER BY a.created_at DESC, a.id
LIMIT 100;

-- Hours logged on a task through timesheets, per person.
-- name: TaskTimeByPerson :many
SELECT u.id, u.name, sum(e.minutes)::int AS minutes
FROM timesheet_entries e
JOIN timesheets ts ON ts.id = e.timesheet_id
JOIN users u ON u.id = ts.user_id
WHERE e.task_id = @task_id
GROUP BY u.id, u.name
ORDER BY minutes DESC;

-- A task key (FE-142) in a team this person belongs to.
-- name: FindMemberTask :one
SELECT k.id, t.key, k.number
FROM tasks k
JOIN teams t ON t.id = k.team_id
JOIN team_members tm ON tm.team_id = t.id AND tm.user_id = @user_id
WHERE t.key = @key AND k.number = @number;

-- name: TeamKeyTaken :one
SELECT EXISTS (SELECT 1 FROM teams WHERE key = @key)::bool;
