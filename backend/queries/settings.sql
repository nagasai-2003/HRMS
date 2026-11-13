-- name: GetSettings :one
SELECT * FROM attendance_settings WHERE id = 1;

-- name: UpdateSettings :one
UPDATE attendance_settings SET
    rule = @rule, max_accuracy_m = @max_accuracy_m, shift_start = @shift_start,
    late_after = @late_after, auto_checkout = @auto_checkout, wfh_quota = @wfh_quota,
    timezone = @timezone, updated_at = now()
WHERE id = 1
RETURNING *;

-- name: ListOffices :many
SELECT * FROM offices ORDER BY name;

-- name: CreateOffice :one
INSERT INTO offices (name, lat, lng, radius_m) VALUES (@name, @lat, @lng, @radius_m) RETURNING *;

-- name: UpdateOffice :one
UPDATE offices SET name = @name, lat = @lat, lng = @lng, radius_m = @radius_m, active = @active
WHERE id = @id RETURNING *;

-- name: DeleteOffice :exec
DELETE FROM offices WHERE id = @id;

-- name: ListNetworks :many
SELECT id, cidr::text AS cidr, label FROM allowed_networks ORDER BY created_at;

-- name: CreateNetwork :one
INSERT INTO allowed_networks (cidr, label) VALUES (@cidr::cidr, @label)
RETURNING id, cidr::text AS cidr, label;

-- name: DeleteNetwork :exec
DELETE FROM allowed_networks WHERE id = @id;

-- name: ListBreakReasons :many
SELECT * FROM break_reasons WHERE active ORDER BY sort, label;

-- name: GetHoliday :one
SELECT * FROM holidays WHERE day = @day;

-- name: ListHolidays :many
SELECT * FROM holidays WHERE day BETWEEN @from_day AND @to_day ORDER BY day;

-- name: UpsertHoliday :exec
INSERT INTO holidays (day, name) VALUES (@day, @name)
ON CONFLICT (day) DO UPDATE SET name = excluded.name;

-- name: DeleteHoliday :exec
DELETE FROM holidays WHERE day = @day;

-- name: ListProjects :many
SELECT * FROM projects ORDER BY active DESC, code;

-- name: CreateProject :one
INSERT INTO projects (code, name) VALUES (@code, @name) RETURNING *;

-- name: UpdateProject :one
UPDATE projects SET code = @code, name = @name, active = @active WHERE id = @id RETURNING *;
