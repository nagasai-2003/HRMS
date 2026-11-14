-- ---- conversations ----

-- Team channels of the teams this person belongs to, with unread counts.
-- name: ListMyTeamChannels :many
SELECT c.id, c.team_id, t.name AS team_name, t.key AS team_key, c.name, c.topic, c.is_default,
    c.archived_at, c.last_message_at, tm.is_manager,
    coalesce(r.last_read_id, 0)::bigint AS last_read_id,
    (SELECT count(*) FROM messages m WHERE m.channel_id = c.id AND m.id > coalesce(r.last_read_id, 0)
        AND m.author_id <> @user_id AND m.deleted_at IS NULL)::int AS unread,
    (SELECT count(*) FROM messages m WHERE m.channel_id = c.id AND m.id > coalesce(r.last_read_id, 0)
        AND m.author_id <> @user_id AND m.deleted_at IS NULL AND @user_id = ANY(m.mentions))::int AS mentioned
FROM channels c
JOIN teams t ON t.id = c.team_id
JOIN team_members tm ON tm.team_id = c.team_id AND tm.user_id = @user_id
LEFT JOIN chat_reads r ON r.channel_id = c.id AND r.user_id = @user_id
WHERE c.kind = 'team'
ORDER BY t.name, c.is_default DESC, c.name;

-- name: ListMyDMs :many
SELECT c.id, c.last_message_at, c.created_at,
    coalesce(r.last_read_id, 0)::bigint AS last_read_id,
    (SELECT count(*) FROM messages m WHERE m.channel_id = c.id AND m.id > coalesce(r.last_read_id, 0)
        AND m.author_id <> @user_id AND m.deleted_at IS NULL)::int AS unread
FROM channels c
JOIN dm_members dm ON dm.channel_id = c.id AND dm.user_id = @user_id
LEFT JOIN chat_reads r ON r.channel_id = c.id AND r.user_id = @user_id
WHERE c.kind = 'dm'
ORDER BY coalesce(c.last_message_at, c.created_at) DESC
LIMIT 200;

-- Everyone in these direct-message conversations.
-- name: ListDMMembersFor :many
SELECT dm.channel_id, u.id, u.name, u.title, u.active,
    coalesce((SELECT string_agg(t.name, ', ' ORDER BY t.name) FROM team_members tm JOIN teams t ON t.id = tm.team_id
              WHERE tm.user_id = u.id), '')::text AS teams
FROM dm_members dm JOIN users u ON u.id = dm.user_id
WHERE dm.channel_id = ANY(@channel_ids::uuid[])
ORDER BY u.name;

-- name: CountUnread :one
SELECT coalesce(sum((SELECT count(*) FROM messages m WHERE m.channel_id = c.id AND m.id > coalesce(r.last_read_id, 0)
        AND m.author_id <> @user_id AND m.deleted_at IS NULL)), 0)::int
FROM channels c
LEFT JOIN chat_reads r ON r.channel_id = c.id AND r.user_id = @user_id
WHERE (c.kind = 'team' AND EXISTS (SELECT 1 FROM team_members tm WHERE tm.team_id = c.team_id AND tm.user_id = @user_id))
   OR (c.kind = 'dm' AND EXISTS (SELECT 1 FROM dm_members dm WHERE dm.channel_id = c.id AND dm.user_id = @user_id));

-- name: GetChannel :one
SELECT c.*, coalesce(t.name, '')::text AS team_name, coalesce(t.key, '')::text AS team_key
FROM channels c LEFT JOIN teams t ON t.id = c.team_id
WHERE c.id = @id;

-- name: IsDMMember :one
SELECT EXISTS (SELECT 1 FROM dm_members WHERE channel_id = @channel_id AND user_id = @user_id)::bool;

-- People who can read a conversation: the team's members, or the DM's members.
-- name: ChannelAudience :many
SELECT tm.user_id FROM channels c JOIN team_members tm ON tm.team_id = c.team_id
WHERE c.id = @id AND c.kind = 'team'
UNION
SELECT dm.user_id FROM dm_members dm WHERE dm.channel_id = @id;

-- name: ListChannelPeople :many
SELECT u.id, u.name, u.title, u.active, coalesce(tm.is_manager, false)::bool AS is_manager
FROM users u
LEFT JOIN team_members tm ON tm.user_id = u.id AND tm.team_id = sqlc.narg(team_id)::uuid
WHERE (sqlc.narg(team_id)::uuid IS NOT NULL AND tm.user_id IS NOT NULL)
   OR (sqlc.narg(team_id)::uuid IS NULL AND u.id IN (SELECT dm.user_id FROM dm_members dm WHERE dm.channel_id = @channel_id))
ORDER BY u.name;

-- name: CountTeamChannels :one
SELECT count(*)::int FROM channels WHERE team_id = @team_id;

-- name: CreateTeamChannel :one
INSERT INTO channels (kind, team_id, name, topic, is_default, created_by)
VALUES ('team', @team_id, @name, @topic, @is_default, @created_by)
RETURNING *;

-- name: UpdateChannel :exec
UPDATE channels SET name = @name, topic = @topic, archived_at = @archived_at WHERE id = @id;

-- name: FindDMByKey :one
SELECT id FROM channels WHERE dm_key = @dm_key;

-- A group conversation with exactly these people.
-- name: FindGroupDM :one
SELECT c.id FROM channels c
WHERE c.kind = 'dm' AND c.dm_key IS NULL
  AND (SELECT array_agg(dm.user_id ORDER BY dm.user_id) FROM dm_members dm WHERE dm.channel_id = c.id) = @member_ids::uuid[]
LIMIT 1;

-- name: CreateDM :one
INSERT INTO channels (kind, dm_key, created_by) VALUES ('dm', sqlc.narg(dm_key), @created_by) RETURNING *;

-- name: AddDMMember :exec
INSERT INTO dm_members (channel_id, user_id) VALUES (@channel_id, @user_id) ON CONFLICT DO NOTHING;

-- name: ActiveUsersByID :many
SELECT id, name FROM users WHERE id = ANY(@ids::uuid[]) AND active;

-- Company directory for starting a conversation.
-- name: SearchPeople :many
SELECT u.id, u.name, u.title,
    coalesce((SELECT string_agg(t.name, ', ' ORDER BY t.name) FROM team_members tm JOIN teams t ON t.id = tm.team_id
              WHERE tm.user_id = u.id), '')::text AS teams
FROM users u
WHERE u.active AND u.id <> @user_id
  AND (@q::text = '' OR u.name ILIKE '%' || @q || '%' OR u.title ILIKE '%' || @q || '%')
ORDER BY u.name
LIMIT 30;

-- New team members start with the team's history read.
-- name: MarkTeamChannelsRead :exec
INSERT INTO chat_reads (channel_id, user_id, last_read_id)
SELECT c.id, @user_id, coalesce((SELECT max(m.id) FROM messages m WHERE m.channel_id = c.id), 0)
FROM channels c WHERE c.team_id = @team_id
ON CONFLICT (channel_id, user_id) DO NOTHING;

-- ---- messages ----

-- name: ListMessages :many
SELECT m.*, u.name AS author_name
FROM messages m JOIN users u ON u.id = m.author_id
WHERE m.channel_id = @channel_id AND (@before::bigint = 0 OR m.id < @before)
ORDER BY m.id DESC
LIMIT @lim;

-- name: ListMessagesAfter :many
SELECT m.*, u.name AS author_name
FROM messages m JOIN users u ON u.id = m.author_id
WHERE m.channel_id = @channel_id AND m.id > @after
ORDER BY m.id
LIMIT 200;

-- name: GetMessage :one
SELECT m.*, u.name AS author_name
FROM messages m JOIN users u ON u.id = m.author_id
WHERE m.id = @id;

-- name: CreateMessage :one
INSERT INTO messages (channel_id, author_id, body, mentions)
VALUES (@channel_id, @author_id, @body, @mentions)
RETURNING *;

-- name: TouchChannel :exec
UPDATE channels SET last_message_at = now() WHERE id = @id;

-- name: EditMessage :exec
UPDATE messages SET body = @body, mentions = @mentions, edited_at = now() WHERE id = @id;

-- name: DeleteMessage :exec
UPDATE messages SET body = '', mentions = '{}', deleted_at = now() WHERE id = @id;

-- Read markers only move forward.
-- name: MarkRead :one
INSERT INTO chat_reads (channel_id, user_id, last_read_id) VALUES (@channel_id, @user_id, @last_read_id)
ON CONFLICT (channel_id, user_id) DO UPDATE SET last_read_id = greatest(chat_reads.last_read_id, excluded.last_read_id)
RETURNING last_read_id;

-- name: GetLastRead :one
SELECT coalesce((SELECT last_read_id FROM chat_reads WHERE channel_id = @channel_id AND user_id = @user_id), 0)::bigint;

-- name: MaxMessageID :one
SELECT coalesce(max(id), 0)::bigint FROM messages WHERE channel_id = @channel_id;

-- name: UserNames :many
SELECT id, name FROM users WHERE id = ANY(@ids::uuid[]);

-- Search in every conversation this person can read.
-- name: SearchMessages :many
SELECT m.id, m.channel_id, m.body, m.mentions, m.created_at, u.name AS author_name,
    c.kind, c.name AS channel_name, coalesce(t.name, '')::text AS team_name
FROM messages m
JOIN channels c ON c.id = m.channel_id
JOIN users u ON u.id = m.author_id
LEFT JOIN teams t ON t.id = c.team_id
WHERE m.deleted_at IS NULL
  AND m.body ILIKE '%' || @q::text || '%' ESCAPE '\'
  AND ((c.kind = 'team' AND EXISTS (SELECT 1 FROM team_members tm WHERE tm.team_id = c.team_id AND tm.user_id = @user_id))
    OR (c.kind = 'dm' AND EXISTS (SELECT 1 FROM dm_members dm WHERE dm.channel_id = c.id AND dm.user_id = @user_id)))
ORDER BY m.id DESC
LIMIT 40;

-- Task cards for keys like FE-6 in messages, for tasks this person can open.
-- name: TasksByRef :many
SELECT k.id, (t.key || '-' || k.number)::text AS ref, k.title,
    s.name AS status_name, s.color AS status_color, s.category AS status_category
FROM tasks k
JOIN teams t ON t.id = k.team_id
JOIN task_statuses s ON s.id = k.status_id
WHERE (t.key || '-' || k.number) = ANY(@refs::text[])
  AND (@all_teams::bool OR EXISTS (SELECT 1 FROM team_members tm WHERE tm.team_id = k.team_id AND tm.user_id = @user_id));

-- ---- notifications ----

-- name: CreateNotification :one
INSERT INTO notifications (user_id, kind, actor_id, title, body, link)
VALUES (@user_id, @kind, @actor_id, @title, @body, @link)
RETURNING *;

-- name: ListNotifications :many
SELECT n.*, coalesce(u.name, '')::text AS actor_name
FROM notifications n LEFT JOIN users u ON u.id = n.actor_id
WHERE n.user_id = @user_id
ORDER BY n.created_at DESC
LIMIT 50;

-- name: CountUnreadNotifications :one
SELECT count(*)::int FROM notifications WHERE user_id = @user_id AND read_at IS NULL;

-- name: ReadNotifications :exec
UPDATE notifications SET read_at = now()
WHERE user_id = @user_id AND read_at IS NULL AND (@every::bool OR id = ANY(@ids::uuid[]));

-- name: DeleteOldNotifications :exec
DELETE FROM notifications WHERE created_at < now() - interval '90 days';
