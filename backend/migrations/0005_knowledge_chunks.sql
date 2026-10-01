-- Traceable chunk index over the extracted teaching text.
--
-- The extracted text keeps its single authority in MySQL: renaming
-- knowledge_entries.content to body_text states that it is the full source a
-- citation is checked against, and the rename is instant — no row is rewritten.
--
-- knowledge_indexes records, per material, which generation of chunks is the
-- live one, what strategy produced it and whether it is pending/ready/failed.
-- Requiring "current generation AND ready AND same class" at query time is what
-- keeps a half-written rebuild unreadable.
--
-- knowledge_chunks holds one row per slice with its Unicode character range.
-- The chunk text lives here and is never copied into the vector store, so a
-- cited excerpt can only ever come from the authoritative row.

ALTER TABLE knowledge_entries RENAME COLUMN content TO body_text;

-- Composite unique key so a chunk's foreign key can carry the class id along
-- with the entry id, which is what makes an out-of-class chunk unrepresentable.
ALTER TABLE knowledge_entries
    ADD UNIQUE KEY uq_knowledge_id_class (id, class_id);

-- One row per material's index state. knowledge_entries is already 1:1 with
-- materials, so keying by knowledge_entry_id is what makes a material's live
-- generation unambiguous.
CREATE TABLE knowledge_indexes (
    knowledge_entry_id BIGINT UNSIGNED NOT NULL,
    class_id           BIGINT UNSIGNED NOT NULL,
    material_id        BIGINT UNSIGNED NOT NULL,
    generation         INT UNSIGNED NOT NULL,
    status             ENUM('pending', 'ready', 'failed') NOT NULL,
    strategy           VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    max_chars          SMALLINT UNSIGNED NOT NULL,
    overlap_percent    TINYINT UNSIGNED NOT NULL,
    `separator`        VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    remove_urls_emails TINYINT(1) NOT NULL,
    fold_whitespace    TINYINT(1) NOT NULL,
    failure_code       VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    created_at         DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
    updated_at         DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
    PRIMARY KEY (knowledge_entry_id),
    KEY idx_knowledge_indexes_class (class_id),
    KEY idx_knowledge_indexes_material (material_id),
    KEY idx_knowledge_indexes_status (status),
    CONSTRAINT fk_knowledge_indexes_entry FOREIGN KEY (knowledge_entry_id, class_id)
        REFERENCES knowledge_entries (id, class_id) ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT fk_knowledge_indexes_material FOREIGN KEY (material_id, class_id)
        REFERENCES materials (id, class_id) ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT chk_knowledge_indexes_strategy
        CHECK (strategy IN ('auto', 'custom', 'hierarchy')),
    CONSTRAINT chk_knowledge_indexes_separator
        CHECK (`separator` IN ('newline', 'blank_line', 'sentence')),
    CONSTRAINT chk_knowledge_indexes_overlap
        CHECK (overlap_percent <= 50)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- start_offset/end_offset are Unicode character positions into the text that was
-- actually chunked (start inclusive, end exclusive); offset_basis says whether
-- that text was the stored body_text or its preprocessed form. They are
-- deliberately not file byte offsets or PDF page numbers.
CREATE TABLE knowledge_chunks (
    id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    class_id           BIGINT UNSIGNED NOT NULL,
    material_id        BIGINT UNSIGNED NOT NULL,
    knowledge_entry_id BIGINT UNSIGNED NOT NULL,
    index_generation   INT UNSIGNED NOT NULL,
    chunk_index        INT UNSIGNED NOT NULL,
    chunk_text         MEDIUMTEXT NOT NULL,
    start_offset       INT UNSIGNED NOT NULL,
    end_offset         INT UNSIGNED NOT NULL,
    offset_basis       ENUM('extracted', 'normalized') NOT NULL,
    index_status       ENUM('pending', 'ready', 'failed') NOT NULL,
    created_at         DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
    PRIMARY KEY (id),
    UNIQUE KEY uq_knowledge_chunks_position (knowledge_entry_id, index_generation, chunk_index),
    KEY idx_knowledge_chunks_class (class_id),
    KEY idx_knowledge_chunks_generation (material_id, index_generation),
    KEY idx_knowledge_chunks_status (knowledge_entry_id, index_generation, index_status),
    CONSTRAINT fk_knowledge_chunks_entry FOREIGN KEY (knowledge_entry_id, class_id)
        REFERENCES knowledge_entries (id, class_id) ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT fk_knowledge_chunks_material FOREIGN KEY (material_id, class_id)
        REFERENCES materials (id, class_id) ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT chk_knowledge_chunks_offsets CHECK (end_offset >= start_offset)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- The keyword path. ngram_token_size is pinned to 2 by the db service command;
-- the parser is part of the index definition, so a query without MATCH ... AGAINST
-- simply does not use it.
ALTER TABLE knowledge_chunks
    ADD FULLTEXT KEY ft_knowledge_chunks_text (chunk_text) WITH PARSER ngram;
