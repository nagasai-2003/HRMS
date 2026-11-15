ALTER TABLE sessions DROP COLUMN stage;
DROP TABLE mfa_recovery_codes;
ALTER TABLE users DROP CONSTRAINT users_owner_active_admin;
DROP INDEX users_one_owner;
ALTER TABLE users
    DROP COLUMN mfa_last_step,
    DROP COLUMN mfa_enabled_at,
    DROP COLUMN mfa_secret,
    DROP COLUMN is_owner;
