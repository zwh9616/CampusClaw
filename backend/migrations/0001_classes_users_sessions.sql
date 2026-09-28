-- Identity and tenancy roots.
--
-- Every table is InnoDB/utf8mb4, every column is NOT NULL, and timestamps are
-- UTC DATETIME(6). Business foreign keys stay RESTRICT so a class or user can
-- never be deleted out from under teaching material.

CREATE TABLE classes (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name       VARCHAR(100) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
    PRIMARY KEY (id),
    UNIQUE KEY uq_classes_name (name)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- username and role use utf8mb4_0900_bin: case-sensitive *and* NO PAD. The
-- table's default collation is case-insensitive, which would let the CHECK
-- accept 'TEACHER', and the legacy *_bin collations are PAD SPACE, which would
-- let it accept 'student '. Both would store a role the Go comparison does not
-- recognise.
--
-- UNIQUE (id, class_id) exists to be the target of the composite
-- materials.uploaded_by foreign key.
CREATE TABLE users (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    username      VARCHAR(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role          VARCHAR(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL,
    class_id      BIGINT UNSIGNED NOT NULL,
    created_at    DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
    PRIMARY KEY (id),
    UNIQUE KEY uq_users_username (username),
    UNIQUE KEY uq_users_id_class (id, class_id),
    KEY idx_users_class (class_id),
    CONSTRAINT fk_users_class FOREIGN KEY (class_id) REFERENCES classes (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT chk_users_role CHECK (role IN ('teacher', 'student'))
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- session_id stores the SHA-256 hex digest of the Cookie token, never the token
-- itself. The ascii_bin collation keeps digest comparison exact and cheap.
CREATE TABLE sessions (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    session_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    user_id    BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
    expires_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_sessions_session_id (session_id),
    KEY idx_sessions_user (user_id),
    KEY idx_sessions_expires (expires_at),
    CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
