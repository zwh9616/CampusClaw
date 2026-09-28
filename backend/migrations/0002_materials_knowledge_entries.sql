-- Teaching material and its extracted text.
--
-- The composite foreign keys are the tenant guard at the storage layer: a
-- material cannot be uploaded by a user from another class, and a knowledge
-- entry cannot belong to a different class than its material.

CREATE TABLE materials (
    id                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    class_id          BIGINT UNSIGNED NOT NULL,
    uploaded_by       BIGINT UNSIGNED NOT NULL,
    title             VARCHAR(255) NOT NULL,
    original_filename VARCHAR(255) NOT NULL,
    stored_filename   VARCHAR(255) NOT NULL,
    content_type      VARCHAR(100) NOT NULL,
    created_at        DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
    PRIMARY KEY (id),
    UNIQUE KEY uq_materials_stored_filename (stored_filename),
    UNIQUE KEY uq_materials_id_class (id, class_id),
    KEY idx_materials_class (class_id),
    KEY idx_materials_uploader_class (uploaded_by, class_id),
    CONSTRAINT fk_materials_class FOREIGN KEY (class_id) REFERENCES classes (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT fk_materials_uploader FOREIGN KEY (uploaded_by, class_id)
        REFERENCES users (id, class_id) ON DELETE RESTRICT ON UPDATE RESTRICT
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- Exactly one extracted-text row per material; no chunking in this iteration.
-- MEDIUMTEXT holds the largest permitted 5 MiB extraction.
CREATE TABLE knowledge_entries (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    class_id    BIGINT UNSIGNED NOT NULL,
    material_id BIGINT UNSIGNED NOT NULL,
    content     MEDIUMTEXT NOT NULL,
    created_at  DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
    PRIMARY KEY (id),
    UNIQUE KEY uq_knowledge_material (material_id),
    KEY idx_knowledge_class (class_id),
    KEY idx_knowledge_material_class (material_id, class_id),
    CONSTRAINT fk_knowledge_class FOREIGN KEY (class_id) REFERENCES classes (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT fk_knowledge_material FOREIGN KEY (material_id, class_id)
        REFERENCES materials (id, class_id) ON DELETE RESTRICT ON UPDATE RESTRICT
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
