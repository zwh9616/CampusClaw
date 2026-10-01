-- The old cookie-only sessions cannot authenticate in the new protocol.
-- Clear them before adding a required independent refresh digest.
DELETE FROM sessions;

ALTER TABLE sessions
    ADD COLUMN refresh_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL AFTER session_id,
    ADD UNIQUE KEY uq_sessions_refresh_id (refresh_id);
