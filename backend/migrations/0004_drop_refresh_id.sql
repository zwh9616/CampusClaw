-- The refresh credential is gone: one access token is now the whole session, so
-- the independent refresh digest has nothing to hold. 0003 introduced the column
-- and is already recorded in schema_migrations, so it cannot be rewritten — this
-- migration retires it instead.
--
-- Existing rows cannot authenticate under the new protocol, and their cookie
-- value must never be replayable as a Bearer token, so clear them before
-- dropping the column. Users sign in once after the upgrade.
DELETE FROM sessions;

-- Dropping the column also drops the single-column unique index on it.
ALTER TABLE sessions
    DROP COLUMN refresh_id;
