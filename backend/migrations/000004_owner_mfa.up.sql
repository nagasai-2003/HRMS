-- Step 4: one protected Owner, and sign-in with an authenticator app for everyone.

ALTER TABLE users
    -- The Owner is an Admin that other Admins cannot change. Exactly one.
    ADD COLUMN is_owner       boolean NOT NULL DEFAULT false,
    -- Authenticator secret, encrypted with MFA_KEY. Set while setting up.
    ADD COLUMN mfa_secret     bytea,
    -- Null until the person has confirmed a first code.
    ADD COLUMN mfa_enabled_at timestamptz,
    -- The last 30-second step used, so a code cannot be used twice.
    ADD COLUMN mfa_last_step  bigint NOT NULL DEFAULT 0;

CREATE UNIQUE INDEX users_one_owner ON users (is_owner) WHERE is_owner;
ALTER TABLE users ADD CONSTRAINT users_owner_active_admin
    CHECK (NOT is_owner OR (role = 'admin' AND active));

-- In a company that already has people, the earliest active Admin becomes the Owner.
UPDATE users SET is_owner = true
WHERE id = (SELECT id FROM users WHERE role = 'admin' AND active ORDER BY created_at, id LIMIT 1);

-- Ten one-time codes for when the phone is lost. Only hashes are kept.
CREATE TABLE mfa_recovery_codes (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  bytea NOT NULL,
    used_at    timestamptz
);
CREATE INDEX mfa_recovery_codes_user ON mfa_recovery_codes (user_id);

-- After the password, a session waits for a code ('code') or for the
-- authenticator to be set up ('setup'). Only 'full' sessions use the app.
ALTER TABLE sessions ADD COLUMN stage text NOT NULL DEFAULT 'full'
    CHECK (stage IN ('full', 'code', 'setup'));

-- Everyone signs in again and sets up their authenticator.
DELETE FROM sessions;
