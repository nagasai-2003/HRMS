-- Step 2: team task boards with custom status groups, linked to timesheets.
-- A team's tasks are visible only to that team's members and to Admin.

-- Every team gets a short key used in task numbers (FE-142).
ALTER TABLE teams ADD COLUMN key text;
ALTER TABLE teams ADD COLUMN task_seq integer NOT NULL DEFAULT 0;

WITH k AS (
    SELECT id,
        upper(left(regexp_replace(name, '[^A-Za-z0-9]', '', 'g'), 3)) AS base,
        row_number() OVER (
            PARTITION BY upper(left(regexp_replace(name, '[^A-Za-z0-9]', '', 'g'), 3))
            ORDER BY created_at, id
        ) AS n
    FROM teams
)
UPDATE teams t SET key =
    CASE WHEN k.base ~ '^[A-Z][A-Z0-9]+$' THEN k.base || CASE WHEN k.n > 1 THEN k.n::text ELSE '' END
         ELSE 'T' || upper(substr(md5(t.id::text), 1, 4)) END
FROM k WHERE k.id = t.id;

ALTER TABLE teams ALTER COLUMN key SET NOT NULL;
ALTER TABLE teams ADD CONSTRAINT teams_key_format CHECK (key ~ '^[A-Z][A-Z0-9]{1,5}$');
CREATE UNIQUE INDEX teams_key_unique ON teams (key);

-- Status groups. Each team names its own; "category" lets dashboards
-- compare teams and decides when a task counts as completed.
CREATE TYPE status_category AS ENUM ('not_started', 'active', 'done');

CREATE TABLE task_statuses (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id     uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    name        text NOT NULL CHECK (length(name) BETWEEN 1 AND 40),
    color       text NOT NULL CHECK (color ~ '^#[0-9A-Fa-f]{6}$'),
    category    status_category NOT NULL,
    position    integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX task_statuses_name ON task_statuses (team_id, lower(name));

CREATE TYPE task_priority AS ENUM ('low', 'medium', 'high', 'urgent');

CREATE TABLE tasks (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id           uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    number            integer NOT NULL,
    title             text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    description       text NOT NULL DEFAULT '',
    -- No cascade: a status group with tasks must be emptied before it goes.
    status_id         uuid NOT NULL REFERENCES task_statuses(id),
    priority          task_priority NOT NULL DEFAULT 'medium',
    labels            text[] NOT NULL DEFAULT '{}',
    start_date        date,
    due_date          date,
    estimate_minutes  integer CHECK (estimate_minutes BETWEEN 1 AND 60000),
    reporter_id       uuid NOT NULL REFERENCES users(id),
    position          double precision NOT NULL DEFAULT 0,
    -- Set when the task enters a Done group, cleared when it leaves.
    completed_at      timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (team_id, number)
);
CREATE INDEX tasks_board ON tasks (team_id, status_id, position);
CREATE INDEX tasks_status ON tasks (status_id);

CREATE TABLE task_assignees (
    task_id  uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    user_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, user_id)
);
CREATE INDEX task_assignees_user ON task_assignees (user_id);

CREATE TABLE task_checklist (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    title       text NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    done        boolean NOT NULL DEFAULT false,
    position    integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX task_checklist_task ON task_checklist (task_id);

CREATE TABLE task_comments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    author_id   uuid NOT NULL REFERENCES users(id),
    body        text NOT NULL CHECK (length(body) BETWEEN 1 AND 4000),
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX task_comments_task ON task_comments (task_id, created_at);

-- Who did what, shown on the task. Text is stored without the actor's name.
CREATE TABLE task_activity (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    actor_id    uuid NOT NULL REFERENCES users(id),
    text        text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX task_activity_task ON task_activity (task_id, created_at);

-- Timesheet rows can point at a task; deleting the task keeps the hours.
ALTER TABLE timesheet_entries ADD COLUMN task_id uuid REFERENCES tasks(id) ON DELETE SET NULL;
CREATE INDEX timesheet_entries_task ON timesheet_entries (task_id) WHERE task_id IS NOT NULL;

-- Existing teams start with the default groups.
INSERT INTO task_statuses (team_id, name, color, category, position)
SELECT t.id, s.name, s.color, s.category::status_category, s.position
FROM teams t
CROSS JOIN (VALUES
    ('To do', '#8A909C', 'not_started', 0),
    ('In progress', '#2F5BD3', 'active', 1),
    ('In review', '#D97706', 'active', 2),
    ('Blocked', '#B42318', 'active', 3),
    ('Done', '#15803D', 'done', 4)
) AS s(name, color, category, position);
