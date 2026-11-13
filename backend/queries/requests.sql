-- name: CreateRequest :one
INSERT INTO requests (user_id, kind, day, reason, fix_in, fix_out, is_extra)
VALUES (@user_id, @kind, @day, @reason, sqlc.narg('fix_in'), sqlc.narg('fix_out'), @is_extra)
RETURNING *;

-- name: GetRequest :one
SELECT * FROM requests WHERE id = @id;

-- name: ListMyRequests :many
SELECT * FROM requests WHERE user_id = @user_id ORDER BY day DESC, created_at DESC LIMIT 200;

-- WFH days that count against the quota: approved ones plus ones still waiting.
-- name: CountWFHInRange :one
SELECT
    count(*) FILTER (WHERE status = 'approved')::int AS approved,
    count(*) FILTER (WHERE status = 'pending')::int AS pending
FROM requests
WHERE user_id = @user_id AND kind = 'wfh' AND day BETWEEN @from_day AND @to_day;

-- name: ApprovedRemoteKind :one
SELECT kind FROM requests
WHERE user_id = @user_id AND day = @day AND status = 'approved' AND kind IN ('wfh', 'on_duty')
ORDER BY kind = 'on_duty' DESC
LIMIT 1;

-- name: ListApprovedRemoteInRange :many
SELECT user_id, day, kind FROM requests
WHERE day BETWEEN @from_day AND @to_day AND status = 'approved' AND kind IN ('wfh', 'on_duty')
  AND (sqlc.narg('user_ids')::uuid[] IS NULL OR user_id = ANY(sqlc.narg('user_ids')::uuid[]));

-- name: CancelRequest :one
UPDATE requests SET status = 'cancelled'
WHERE id = @id AND user_id = @user_id AND status IN ('pending', 'approved')
RETURNING *;

-- name: DecideRequest :one
UPDATE requests SET status = @status, decided_by = @decided_by, decided_at = now(), decision_note = @decision_note
WHERE id = @id AND status = 'pending'
RETURNING *;

-- name: ListRequestsForReview :many
SELECT r.*, u.name AS user_name, coalesce(d.name, '')::text AS decided_by_name
FROM requests r
JOIN users u ON u.id = r.user_id
LEFT JOIN users d ON d.id = r.decided_by
WHERE (sqlc.narg('status')::text IS NULL OR r.status = sqlc.narg('status')::text)
  AND (sqlc.narg('user_ids')::uuid[] IS NULL OR r.user_id = ANY(sqlc.narg('user_ids')::uuid[]))
ORDER BY (r.status = 'pending') DESC, r.day DESC, r.created_at DESC
LIMIT 300;
