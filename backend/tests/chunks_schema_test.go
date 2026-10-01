package tests

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// insertKnowledgeEntry creates the extracted-text row for a material.
func insertKnowledgeEntry(t *testing.T, handle *sql.DB, classID, materialID uint64, body string) uint64 {
	t.Helper()

	result, err := handle.Exec(
		"INSERT INTO knowledge_entries (class_id, material_id, body_text) VALUES (?, ?, ?)",
		classID, materialID, body,
	)
	if err != nil {
		t.Fatalf("insert knowledge entry: %v", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("knowledge entry id: %v", err)
	}
	return uint64(id)
}

// insertChunk writes one chunk row with sensible defaults.
func insertChunk(t *testing.T, handle *sql.DB, classID, materialID, entryID uint64, generation, index int) error {
	t.Helper()

	_, err := handle.Exec(`
		INSERT INTO knowledge_chunks
			(class_id, material_id, knowledge_entry_id, index_generation, chunk_index,
			 chunk_text, start_offset, end_offset, offset_basis, index_status)
		VALUES (?, ?, ?, ?, ?, 'slice', 0, 5, 'extracted', 'pending')`,
		classID, materialID, entryID, generation, index,
	)
	return err
}

// The rename must keep the extracted text addressable under its new name and
// drop the old one, so a reader written against either name is unambiguous.
func TestKnowledgeTextColumnWasRenamedInPlace(t *testing.T) {
	env := NewEnv(t)

	classID := insertClass(t, env.DB, "Class A")
	teacherID := insertUser(t, env.DB, "teacher_a", "teacher", classID)
	materialID := insertMaterial(t, env.DB, classID, teacherID, "store-rename")

	const body = "renamed body text"
	insertKnowledgeEntry(t, env.DB, classID, materialID, body)

	var stored string
	if err := env.DB.QueryRow(
		"SELECT body_text FROM knowledge_entries WHERE material_id = ?", materialID,
	).Scan(&stored); err != nil {
		t.Fatalf("read body_text: %v", err)
	}
	if stored != body {
		t.Errorf("body_text = %q, want %q", stored, body)
	}

	if _, err := env.DB.Exec(
		"SELECT content FROM knowledge_entries WHERE material_id = ?", materialID,
	); err == nil {
		t.Error("the old content column still exists; the rename did not take effect")
	}
}

// The stored text is the one thing a citation is checked against, so the
// migration must not have truncated or rewritten it.
func TestMaterialDetailStillServesTheOriginalText(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)
	body := strings.Repeat("本班教材正文。", 400)
	id := uploadMarkdown(t, env, teacher, "长文本", body)

	detail := env.get(t, "/api/materials/"+id, teacher)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail: status = %d, body = %s", detail.Code, detail.Body.String())
	}

	var decoded detailBody
	if err := json.Unmarshal(detail.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if decoded.Content != body {
		t.Error("material detail no longer returns the stored text unchanged after the rename")
	}
}

// A chunk may only belong to its own class: the composite foreign key makes a
// cross-class chunk unrepresentable rather than merely rejected by the handler.
func TestChunkCannotCrossAClassBoundary(t *testing.T) {
	env := NewEnv(t)

	classA := insertClass(t, env.DB, "Class A")
	classB := insertClass(t, env.DB, "Class B")
	teacherA := insertUser(t, env.DB, "teacher_a", "teacher", classA)
	materialA := insertMaterial(t, env.DB, classA, teacherA, "store-a")
	entryA := insertKnowledgeEntry(t, env.DB, classA, materialA, "text")

	// A chunk claiming Class B for a Class A entry must be refused.
	if err := insertChunk(t, env.DB, classB, materialA, entryA, 1, 0); err == nil {
		t.Error("a Class B chunk referenced a Class A knowledge entry, want a foreign-key error")
	}

	// Naming another class's material is refused for the same reason.
	if err := insertChunk(t, env.DB, classA, materialA+9999, entryA, 1, 0); err == nil {
		t.Error("a chunk referenced a missing material, want a foreign-key error")
	}

	if count := env.CountRows(t, "knowledge_chunks"); count != 0 {
		t.Errorf("knowledge_chunks = %d, want 0 rejected rows", count)
	}
}

// Rebuilding keeps several generations side by side while the old one is being
// drained, so the same position may repeat across generations but never within
// one.
func TestChunkPositionIsUniquePerGeneration(t *testing.T) {
	env := NewEnv(t)

	classID := insertClass(t, env.DB, "Class A")
	teacherID := insertUser(t, env.DB, "teacher_a", "teacher", classID)
	materialID := insertMaterial(t, env.DB, classID, teacherID, "store-a")
	entryID := insertKnowledgeEntry(t, env.DB, classID, materialID, "text")

	if err := insertChunk(t, env.DB, classID, materialID, entryID, 1, 0); err != nil {
		t.Fatalf("first chunk: %v", err)
	}

	if err := insertChunk(t, env.DB, classID, materialID, entryID, 1, 0); err == nil {
		t.Error("a duplicate chunk position within one generation was accepted")
	}

	if err := insertChunk(t, env.DB, classID, materialID, entryID, 2, 0); err != nil {
		t.Errorf("the same position in a new generation was refused: %v", err)
	}
}

// A range that ends before it starts could never select the text it claims to.
func TestChunkOffsetsMustBeOrdered(t *testing.T) {
	env := NewEnv(t)

	classID := insertClass(t, env.DB, "Class A")
	teacherID := insertUser(t, env.DB, "teacher_a", "teacher", classID)
	materialID := insertMaterial(t, env.DB, classID, teacherID, "store-a")
	entryID := insertKnowledgeEntry(t, env.DB, classID, materialID, "text")

	if _, err := env.DB.Exec(`
		INSERT INTO knowledge_chunks
			(class_id, material_id, knowledge_entry_id, index_generation, chunk_index,
			 chunk_text, start_offset, end_offset, offset_basis, index_status)
		VALUES (?, ?, ?, 1, 0, 'slice', 10, 4, 'extracted', 'pending')`,
		classID, materialID, entryID,
	); err == nil {
		t.Error("a chunk with end_offset < start_offset was accepted")
	}
}

// Status is a closed set: an unknown value must not become a chunk a query can
// silently treat as indexed.
func TestChunkStatusRejectsUnknownValues(t *testing.T) {
	env := NewEnv(t)

	classID := insertClass(t, env.DB, "Class A")
	teacherID := insertUser(t, env.DB, "teacher_a", "teacher", classID)
	materialID := insertMaterial(t, env.DB, classID, teacherID, "store-a")
	entryID := insertKnowledgeEntry(t, env.DB, classID, materialID, "text")

	if _, err := env.DB.Exec(`
		INSERT INTO knowledge_chunks
			(class_id, material_id, knowledge_entry_id, index_generation, chunk_index,
			 chunk_text, start_offset, end_offset, offset_basis, index_status)
		VALUES (?, ?, ?, 1, 0, 'slice', 0, 5, 'extracted', 'indexed')`,
		classID, materialID, entryID,
	); err == nil {
		t.Error("an unknown index_status was accepted")
	}
}

// The keyword path depends on the ngram parser being part of the index, not on
// a session setting: a plain MATCH would otherwise tokenise Chinese text into
// nothing.
func TestChunkFulltextIndexUsesTheNgramParser(t *testing.T) {
	env := NewEnv(t)

	var table, create string
	if err := env.DB.QueryRow("SHOW CREATE TABLE knowledge_chunks").Scan(&table, &create); err != nil {
		t.Fatalf("show create table: %v", err)
	}

	// MySQL renders the parser clause as a versioned comment, with the parser
	// name quoted, so the two parts are checked rather than one exact string.
	if !strings.Contains(create, "WITH PARSER") || !strings.Contains(create, "ngram") {
		t.Errorf("knowledge_chunks fulltext index does not use the ngram parser:\n%s", create)
	}
}
