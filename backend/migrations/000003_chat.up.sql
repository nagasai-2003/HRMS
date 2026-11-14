-- Step 3: chat (team channels and direct messages), @mentions and notifications.
--
-- Team channels are private to the team's members (Admin has no special view).
-- Direct messages work across teams: 1:1 (dm_key keeps one per pair) or a
-- group of up to 9 people.

CREATE TYPE channel_kind AS ENUM ('team', 'dm');

CREATE TABLE channels (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind             channel_kind NOT NULL,
    team_id          uuid REFERENCES teams(id) ON DELETE CASCADE,
    name             text NOT NULL DEFAULT '',
    topic            text NOT NULL DEFAULT '',
    -- The team's first channel: it cannot be archived.
    is_default       boolean NOT NULL DEFAULT false,
    -- 1:1 messages: "<lower uuid>:<higher uuid>".
    dm_key           text,
    created_by       uuid REFERENCES users(id) ON DELETE SET NULL,
    archived_at      timestamptz,
    last_message_at  timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT channels_team_kind CHECK ((kind = 'team') = (team_id IS NOT NULL)),
    CONSTRAINT channels_name_format CHECK (kind <> 'team' OR name ~ '^[a-z0-9][a-z0-9-]{0,39}$'),
    CONSTRAINT channels_topic_len CHECK (char_length(topic) <= 250)
);
CREATE UNIQUE INDEX channels_team_name ON channels (team_id, name) WHERE kind = 'team';
CREATE UNIQUE INDEX channels_dm_key ON channels (dm_key) WHERE dm_key IS NOT NULL;

CREATE TABLE dm_members (
    channel_id  uuid NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (channel_id, user_id)
);
CREATE INDEX dm_members_user ON dm_members (user_id);

CREATE TABLE messages (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    channel_id  uuid NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    author_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Mentions are written <@user-id> in the text.
    body        text NOT NULL,
    mentions    uuid[] NOT NULL DEFAULT '{}',
    edited_at   timestamptz,
    deleted_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT messages_body_len CHECK (char_length(body) <= 4000)
);
CREATE INDEX messages_channel ON messages (channel_id, id DESC);

-- The newest message each person has read in each conversation.
CREATE TABLE chat_reads (
    channel_id    uuid NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    last_read_id  bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (channel_id, user_id)
);

CREATE TABLE notifications (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- task_assigned, task_mention, task_comment, chat_mention
    kind        text NOT NULL,
    actor_id    uuid REFERENCES users(id) ON DELETE SET NULL,
    title       text NOT NULL,
    body        text NOT NULL DEFAULT '',
    link        text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    read_at     timestamptz
);
CREATE INDEX notifications_user ON notifications (user_id, created_at DESC);
CREATE INDEX notifications_unread ON notifications (user_id) WHERE read_at IS NULL;

-- Task comments can mention team members too.
ALTER TABLE task_comments ADD COLUMN mentions uuid[] NOT NULL DEFAULT '{}';

-- Every existing team gets its general channel ("frontend-general").
INSERT INTO channels (kind, team_id, name, is_default)
SELECT 'team', t.id,
    left(coalesce(nullif(trim(both '-' from lower(regexp_replace(t.name, '[^A-Za-z0-9]+', '-', 'g'))), ''), 'team'), 31) || '-general',
    true
FROM teams t;
