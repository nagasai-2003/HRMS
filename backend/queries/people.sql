-- name: GetUserByEmail :one
SELECT * FROM users WHERE lower(email) = lower(@email);

-- name: GetUser :one
SELECT * FROM users WHERE id = @id;

-- name: ListUsers :many
SELECT * FROM users ORDER BY active DESC, name;

-- name: CreateUser :one
INSERT INTO users (email, name, title, password_hash, role, work_policy, remote_weekdays, office_id)
VALUES (@email, @name, @title, @password_hash, @role, @work_policy, @remote_weekdays, sqlc.narg('office_id'))
RETURNING *;

-- name: UpdateUser :one
UPDATE users SET
    name = @name, title = @title, role = @role, work_policy = @work_policy,
    remote_weekdays = @remote_weekdays, office_id = sqlc.narg('office_id'), active = @active
WHERE id = @id
RETURNING *;

-- name: SetUserRole :exec
UPDATE users SET role = @role WHERE id = @id;

-- name: SetPassword :exec
UPDATE users SET password_hash = @password_hash WHERE id = @id;

-- ---- Owner ----

-- name: ClearOwner :exec
UPDATE users SET is_owner = false WHERE is_owner;

-- name: MakeOwner :execrows
UPDATE users SET is_owner = true WHERE id = @id AND role = 'admin' AND active;

-- ---- Authenticator ----

-- A new secret, not in use until a first code confirms it.
-- name: SetMFASecret :exec
UPDATE users SET mfa_secret = @mfa_secret, mfa_enabled_at = NULL, mfa_last_step = 0 WHERE id = @id;

-- name: EnableMFA :execrows
UPDATE users SET mfa_enabled_at = now(), mfa_last_step = @step
WHERE id = @id AND mfa_secret IS NOT NULL AND mfa_enabled_at IS NULL;

-- Claims a 30-second step; 0 rows means the code was already used.
-- name: UseMFAStep :execrows
UPDATE users SET mfa_last_step = @step WHERE id = @id AND mfa_last_step < @step;

-- name: ResetMFA :exec
UPDATE users SET mfa_secret = NULL, mfa_enabled_at = NULL, mfa_last_step = 0 WHERE id = @id;

-- name: DeleteRecoveryCodes :exec
DELETE FROM mfa_recovery_codes WHERE user_id = @user_id;

-- name: AddRecoveryCode :exec
INSERT INTO mfa_recovery_codes (user_id, code_hash) VALUES (@user_id, @code_hash);

-- name: UseRecoveryCode :execrows
UPDATE mfa_recovery_codes SET used_at = now()
WHERE user_id = @user_id AND code_hash = @code_hash AND used_at IS NULL;

-- name: CountRecoveryCodesLeft :one
SELECT count(*)::int FROM mfa_recovery_codes WHERE user_id = @user_id AND used_at IS NULL;

-- name: CreateSession :exec
INSERT INTO sessions (token_hash, user_id, expires_at, ip, user_agent, stage)
VALUES (@token_hash, @user_id, @expires_at, @ip, @user_agent, @stage);

-- Only sessions that passed the authenticator code use the app.
-- name: GetSessionUser :one
SELECT u.* FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.token_hash = @token_hash AND s.expires_at > now() AND u.active AND s.stage = 'full';

-- A sign-in waiting for a code or for the authenticator to be set up.
-- name: GetPendingSession :one
SELECT sqlc.embed(u), s.stage FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.token_hash = @token_hash AND s.expires_at > now() AND u.active AND s.stage <> 'full';

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = @token_hash;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = @user_id;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at < now();

-- name: ListTeams :many
SELECT t.id, t.name, t.key,
    count(tm.user_id)::int AS members,
    coalesce(string_agg(u.name, ', ' ORDER BY u.name) FILTER (WHERE tm.is_manager), '')::text AS managers
FROM teams t
LEFT JOIN team_members tm ON tm.team_id = t.id
LEFT JOIN users u ON u.id = tm.user_id
GROUP BY t.id
ORDER BY t.name;

-- name: CreateTeam :one
INSERT INTO teams (name, key) VALUES (@name, @key) RETURNING *;

-- name: UpdateTeam :exec
UPDATE teams SET name = @name, key = @key WHERE id = @id;

-- name: DeleteTeam :exec
DELETE FROM teams WHERE id = @id;

-- name: ListTeamMembers :many
SELECT u.id, u.name, u.email, u.title, u.role, u.work_policy, u.active, tm.is_manager
FROM team_members tm JOIN users u ON u.id = tm.user_id
WHERE tm.team_id = @team_id
ORDER BY tm.is_manager DESC, u.name;

-- name: UpsertTeamMember :exec
INSERT INTO team_members (team_id, user_id, is_manager) VALUES (@team_id, @user_id, @is_manager)
ON CONFLICT (team_id, user_id) DO UPDATE SET is_manager = excluded.is_manager;

-- name: RemoveTeamMember :exec
DELETE FROM team_members WHERE team_id = @team_id AND user_id = @user_id;

-- name: CountManagedTeams :one
SELECT count(*)::int FROM team_members WHERE user_id = @user_id AND is_manager;

-- name: ListUserTeams :many
SELECT t.id, t.name, t.key, tm.is_manager
FROM team_members tm JOIN teams t ON t.id = tm.team_id
WHERE tm.user_id = @user_id
ORDER BY t.name;

-- name: ListAllMemberships :many
SELECT tm.user_id, t.id AS team_id, t.name AS team_name, tm.is_manager
FROM team_members tm JOIN teams t ON t.id = tm.team_id
ORDER BY t.name;

-- People in the teams this person manages (includes the manager).
-- name: ManagedUserIDs :many
SELECT DISTINCT member.user_id
FROM team_members mgr
JOIN team_members member ON member.team_id = mgr.team_id
WHERE mgr.user_id = @manager_id AND mgr.is_manager;

-- Managers of any team this person is in, other than the person.
-- name: ManagersOfUser :many
SELECT DISTINCT mgr.user_id
FROM team_members member
JOIN team_members mgr ON mgr.team_id = member.team_id AND mgr.is_manager
WHERE member.user_id = @user_id AND mgr.user_id <> @user_id;
