-- name: InsertEvent :one
INSERT INTO attendance_events (user_id, day, kind, at, mode, reason, note, lat, lng, accuracy_m, ip, office_id, distance_m, auto)
VALUES (@user_id, @day, @kind, @at, @mode, @reason, @note, sqlc.narg('lat'), sqlc.narg('lng'),
        sqlc.narg('accuracy_m'), @ip, sqlc.narg('office_id'), sqlc.narg('distance_m'), @auto)
RETURNING *;

-- name: ListEventsRange :many
SELECT * FROM attendance_events
WHERE user_id = @user_id AND day BETWEEN @from_day AND @to_day
ORDER BY at;

-- name: ListEventsForDay :many
SELECT * FROM attendance_events
WHERE day = @day AND (sqlc.narg('user_ids')::uuid[] IS NULL OR user_id = ANY(sqlc.narg('user_ids')::uuid[]))
ORDER BY at;

-- name: ListEventsAllRange :many
SELECT * FROM attendance_events
WHERE day BETWEEN @from_day AND @to_day
  AND (sqlc.narg('user_ids')::uuid[] IS NULL OR user_id = ANY(sqlc.narg('user_ids')::uuid[]))
ORDER BY at;

-- name: DeleteDayEvents :exec
DELETE FROM attendance_events WHERE user_id = @user_id AND day = @day;

-- Last event of each person-day before @before, for closing forgotten days.
-- name: LastEventsBefore :many
SELECT DISTINCT ON (user_id, day) user_id, day, kind, at
FROM attendance_events
WHERE day < @before AND day >= @since
ORDER BY user_id, day, at DESC;

-- name: InsertBlocked :one
INSERT INTO attendance_blocked (user_id, day, action, code, message, lat, lng, accuracy_m, distance_m, ip)
VALUES (@user_id, @day, @action, @code, @message, sqlc.narg('lat'), sqlc.narg('lng'),
        sqlc.narg('accuracy_m'), sqlc.narg('distance_m'), @ip)
RETURNING *;

-- name: ListBlockedForDay :many
SELECT b.*, u.name AS user_name
FROM attendance_blocked b JOIN users u ON u.id = b.user_id
WHERE b.day = @day AND (sqlc.narg('user_ids')::uuid[] IS NULL OR b.user_id = ANY(sqlc.narg('user_ids')::uuid[]))
ORDER BY b.at DESC;

-- name: ListMyBlocked :many
SELECT * FROM attendance_blocked WHERE user_id = @user_id AND day = @day ORDER BY at DESC;
