-- Step 1: people, roles, teams, attendance rules, requests and timesheets.

CREATE TYPE app_role AS ENUM ('employee', 'manager', 'hr', 'admin');
-- office: comes in every working day (WFH needs a request).
-- hybrid: remote on the weekdays in users.remote_weekdays, office otherwise.
-- remote: never needs the office network.
CREATE TYPE work_policy AS ENUM ('office', 'hybrid', 'remote');

CREATE TABLE offices (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL UNIQUE,
    lat         double precision NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lng         double precision NOT NULL CHECK (lng BETWEEN -180 AND 180),
    radius_m    integer NOT NULL CHECK (radius_m BETWEEN 20 AND 5000),
    active      boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Public IPs or ranges of the office internet connections.
CREATE TABLE allowed_networks (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    cidr        cidr NOT NULL UNIQUE,
    label       text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- One row (id = 1). Times are in the company time zone.
CREATE TABLE attendance_settings (
    id                 smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    rule               text NOT NULL DEFAULT 'both' CHECK (rule IN ('both', 'either')),
    max_accuracy_m     integer NOT NULL DEFAULT 100 CHECK (max_accuracy_m BETWEEN 10 AND 2000),
    shift_start        time NOT NULL DEFAULT '09:30',
    late_after         time NOT NULL DEFAULT '09:45',
    auto_checkout      time NOT NULL DEFAULT '23:59',
    wfh_quota          integer NOT NULL DEFAULT 2 CHECK (wfh_quota BETWEEN 0 AND 31),
    timezone           text NOT NULL DEFAULT 'Asia/Kolkata',
    updated_at         timestamptz NOT NULL DEFAULT now()
);
INSERT INTO attendance_settings (id) VALUES (1);

CREATE TABLE break_reasons (
    id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    label   text NOT NULL UNIQUE,
    sort    integer NOT NULL DEFAULT 0,
    active  boolean NOT NULL DEFAULT true
);
INSERT INTO break_reasons (label, sort) VALUES
  ('Lunch', 1), ('Tea / coffee', 2), ('Personal', 3),
  ('Meeting outside', 4), ('Medical', 5), ('Other', 6);

CREATE TABLE holidays (
    day   date PRIMARY KEY,
    name  text NOT NULL
);

CREATE TABLE users (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email            text NOT NULL,
    name             text NOT NULL,
    title            text NOT NULL DEFAULT '',
    password_hash    text NOT NULL,
    role             app_role NOT NULL DEFAULT 'employee',
    work_policy      work_policy NOT NULL DEFAULT 'office',
    -- ISO weekdays (1 = Monday ... 7 = Sunday) a hybrid person works remotely.
    remote_weekdays  smallint[] NOT NULL DEFAULT '{}',
    office_id        uuid REFERENCES offices(id) ON DELETE SET NULL,
    active           boolean NOT NULL DEFAULT true,
    created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

CREATE TABLE teams (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX teams_name_key ON teams (lower(name));

-- A person can be in many teams. is_manager marks the team's manager(s):
-- they see that team's attendance, requests and timesheets, and approve its
-- timesheets.
CREATE TABLE team_members (
    team_id     uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    is_manager  boolean NOT NULL DEFAULT false,
    PRIMARY KEY (team_id, user_id)
);
CREATE INDEX team_members_user ON team_members (user_id);

CREATE TABLE sessions (
    token_hash  bytea PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    ip          text NOT NULL DEFAULT '',
    user_agent  text NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions (user_id);

-- Every successful check-in, break and check-out. Times come from the server.
CREATE TABLE attendance_events (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day          date NOT NULL,
    kind         text NOT NULL CHECK (kind IN ('check_in', 'break_start', 'break_end', 'check_out')),
    at           timestamptz NOT NULL DEFAULT now(),
    -- office, wfh, hybrid_remote, remote or on_duty: why the check passed.
    mode         text NOT NULL,
    reason       text NOT NULL DEFAULT '',
    note         text NOT NULL DEFAULT '',
    lat          double precision,
    lng          double precision,
    accuracy_m   double precision,
    ip           text NOT NULL DEFAULT '',
    office_id    uuid REFERENCES offices(id) ON DELETE SET NULL,
    distance_m   double precision,
    -- true when the system closed a day nobody checked out of.
    auto         boolean NOT NULL DEFAULT false
);
CREATE INDEX attendance_events_user_day ON attendance_events (user_id, day, at);
CREATE INDEX attendance_events_day ON attendance_events (day);

-- Refused actions, for HR and managers.
CREATE TABLE attendance_blocked (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day         date NOT NULL,
    action      text NOT NULL,
    at          timestamptz NOT NULL DEFAULT now(),
    code        text NOT NULL,
    message     text NOT NULL,
    lat         double precision,
    lng         double precision,
    accuracy_m  double precision,
    distance_m  double precision,
    ip          text NOT NULL DEFAULT ''
);
CREATE INDEX attendance_blocked_day ON attendance_blocked (day);

-- WFH, on-duty and attendance corrections. HR or Admin decide.
CREATE TABLE requests (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind           text NOT NULL CHECK (kind IN ('wfh', 'on_duty', 'correction')),
    day            date NOT NULL,
    reason         text NOT NULL,
    -- correction only: the times the person says are right.
    fix_in         time,
    fix_out        time,
    status         text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
    -- WFH over the monthly quota at the time it was asked for.
    is_extra       boolean NOT NULL DEFAULT false,
    decided_by     uuid REFERENCES users(id) ON DELETE SET NULL,
    decided_at     timestamptz,
    decision_note  text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX requests_user_day ON requests (user_id, day);
CREATE INDEX requests_status ON requests (status, created_at);
-- One live request of a kind per person per day.
CREATE UNIQUE INDEX requests_one_live ON requests (user_id, kind, day)
    WHERE status IN ('pending', 'approved');

CREATE TABLE projects (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code        text NOT NULL,
    name        text NOT NULL,
    active      boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX projects_code_key ON projects (lower(code));

-- One timesheet per person per week (week_start is a Monday).
CREATE TABLE timesheets (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    week_start     date NOT NULL CHECK (extract(isodow FROM week_start) = 1),
    status         text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'submitted', 'approved', 'rejected')),
    submitted_at   timestamptz,
    decided_by     uuid REFERENCES users(id) ON DELETE SET NULL,
    decided_at     timestamptz,
    decision_note  text NOT NULL DEFAULT '',
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, week_start)
);
CREATE INDEX timesheets_status ON timesheets (status, week_start);

CREATE TABLE timesheet_entries (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    timesheet_id  uuid NOT NULL REFERENCES timesheets(id) ON DELETE CASCADE,
    day           date NOT NULL,
    project_id    uuid NOT NULL REFERENCES projects(id),
    -- Free text for now (e.g. FE-142); becomes a task link in the tasks step.
    task_ref      text NOT NULL DEFAULT '',
    minutes       integer NOT NULL CHECK (minutes BETWEEN 1 AND 1440),
    note          text NOT NULL DEFAULT '',
    sort          integer NOT NULL DEFAULT 0
);
CREATE INDEX timesheet_entries_sheet ON timesheet_entries (timesheet_id, day);
