package tests

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"testing"
	"time"

	"campusclaw/internal/db"
)

// insertClass creates a class and returns its id.
func insertClass(t *testing.T, handle *sql.DB, name string) uint64 {
	t.Helper()

	result, err := handle.Exec("INSERT INTO classes (name) VALUES (?)", name)
	if err != nil {
		t.Fatalf("insert class %q: %v", name, err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("class id: %v", err)
	}
	return uint64(id)
}

// insertUser creates a user and returns its id.
func insertUser(t *testing.T, handle *sql.DB, username, role string, classID uint64) uint64 {
	t.Helper()

	result, err := handle.Exec(
		"INSERT INTO users (username, password_hash, role, class_id) VALUES (?, ?, ?, ?)",
		username, "$2a$12$0123456789012345678901234567890123456789012345678901234567", role, classID,
	)
	if err != nil {
		t.Fatalf("insert user %q: %v", username, err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("user id: %v", err)
	}
	return uint64(id)
}

func insertMaterial(t *testing.T, handle *sql.DB, classID, uploadedBy uint64, storedName string) uint64 {
	t.Helper()

	result, err := handle.Exec(
		`INSERT INTO materials (class_id, uploaded_by, title, original_filename, stored_filename, content_type)
		 VALUES (?, ?, 'title', 'notes.md', ?, 'text/markdown')`,
		classID, uploadedBy, storedName,
	)
	if err != nil {
		t.Fatalf("insert material: %v", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("material id: %v", err)
	}
	return uint64(id)
}

// DA01: the exact table set, including the migration bookkeeping table.
func TestSchemaContainsOnlyTheAgreedTables(t *testing.T) {
	env := NewEnv(t)

	rows, err := env.DB.Query("SHOW TABLES")
	if err != nil {
		t.Fatalf("show tables: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		got = append(got, name)
	}

	sort.Strings(got)

	want := append([]string(nil), businessTables...)
	sort.Strings(want)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tables = %v, want %v (no tables for Non-goals features)", got, want)
	}
}

// DA01: every required column is NOT NULL, so no calculation can omit a
// tenant or an owner.
func TestSchemaColumnsAreNotNull(t *testing.T) {
	env := NewEnv(t)

	required := map[string][]string{
		"classes":           {"id", "name", "created_at"},
		"users":             {"id", "username", "password_hash", "role", "class_id", "created_at"},
		"sessions":          {"id", "session_id", "user_id", "created_at", "expires_at"},
		"materials":         {"id", "class_id", "uploaded_by", "title", "original_filename", "stored_filename", "content_type", "created_at"},
		"knowledge_entries": {"id", "class_id", "material_id", "body_text", "created_at"},
		"knowledge_indexes": {
			"knowledge_entry_id", "class_id", "material_id", "generation", "status", "strategy",
			"max_chars", "overlap_percent", "separator", "remove_urls_emails", "fold_whitespace",
			"failure_code", "created_at", "updated_at",
		},
		"knowledge_chunks": {
			"id", "class_id", "material_id", "knowledge_entry_id", "index_generation", "chunk_index",
			"chunk_text", "start_offset", "end_offset", "offset_basis", "index_status", "created_at",
		},
	}

	for table, columns := range required {
		for _, column := range columns {
			var nullable string
			err := env.DB.QueryRow(`
				SELECT IS_NULLABLE
				  FROM information_schema.columns
				 WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
				table, column,
			).Scan(&nullable)

			switch {
			case err == sql.ErrNoRows:
				t.Errorf("%s.%s is missing", table, column)
			case err != nil:
				t.Fatalf("inspect %s.%s: %v", table, column, err)
			case nullable != "NO":
				t.Errorf("%s.%s is nullable, want NOT NULL", table, column)
			}
		}
	}
}

// DA01: the indexes the design calls for are present.
func TestSchemaHasRequiredIndexes(t *testing.T) {
	env := NewEnv(t)

	required := map[string][]string{
		"classes":           {"PRIMARY", "uq_classes_name"},
		"users":             {"PRIMARY", "uq_users_username", "uq_users_id_class", "idx_users_class"},
		"sessions":          {"PRIMARY", "uq_sessions_session_id", "idx_sessions_user", "idx_sessions_expires"},
		"materials":         {"PRIMARY", "uq_materials_stored_filename", "uq_materials_id_class", "idx_materials_class"},
		"knowledge_entries": {"PRIMARY", "uq_knowledge_material", "idx_knowledge_class", "uq_knowledge_id_class"},
		"knowledge_indexes": {"PRIMARY", "idx_knowledge_indexes_class", "idx_knowledge_indexes_status"},
		"knowledge_chunks": {
			"PRIMARY", "uq_knowledge_chunks_position", "ft_knowledge_chunks_text",
			"idx_knowledge_chunks_class", "idx_knowledge_chunks_status",
		},
	}

	for table, indexes := range required {
		present := map[string]bool{}
		rows, err := env.DB.Query(`
			SELECT DISTINCT index_name
			  FROM information_schema.statistics
			 WHERE table_schema = DATABASE() AND table_name = ?`, table)
		if err != nil {
			t.Fatalf("inspect indexes of %s: %v", table, err)
		}

		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				t.Fatalf("scan index name: %v", err)
			}
			present[name] = true
		}
		rows.Close()

		for _, index := range indexes {
			if !present[index] {
				t.Errorf("%s is missing index %s (have %v)", table, index, present)
			}
		}
	}
}

// Applying migrations twice must be a no-op, not an error.
func TestMigrationsAreRepeatable(t *testing.T) {
	env := NewEnv(t)

	// NewEnv already migrated once; run the same entry point again.
	if err := migrateAgain(t, env); err != nil {
		t.Fatalf("second migration pass: %v", err)
	}

	if count := env.CountRows(t, "classes"); count != 0 {
		t.Errorf("classes after re-migration = %d, want 0", count)
	}
}

func TestUsernameIsUnique(t *testing.T) {
	env := NewEnv(t)

	classID := insertClass(t, env.DB, "Class A")
	insertUser(t, env.DB, "duplicate_name", "student", classID)

	if _, err := env.DB.Exec(
		"INSERT INTO users (username, password_hash, role, class_id) VALUES (?, ?, ?, ?)",
		"duplicate_name", "hash", "student", classID,
	); err == nil {
		t.Error("a duplicate username was accepted, want a unique-constraint error")
	}
}

func TestUsernameComparisonIsCaseSensitive(t *testing.T) {
	env := NewEnv(t)

	classID := insertClass(t, env.DB, "Class A")
	insertUser(t, env.DB, "case_test", "student", classID)

	// The column uses a binary collation, so a different case is a new name.
	insertUser(t, env.DB, "CASE_TEST", "student", classID)
}

func TestRoleIsRestrictedToTeacherOrStudent(t *testing.T) {
	env := NewEnv(t)

	classID := insertClass(t, env.DB, "Class A")

	for _, role := range []string{"admin", "TEACHER", "", "superuser", "student "} {
		if _, err := env.DB.Exec(
			"INSERT INTO users (username, password_hash, role, class_id) VALUES (?, ?, ?, ?)",
			"role_"+role, "hash", role, classID,
		); err == nil {
			t.Errorf("role %q was accepted, want the CHECK constraint to reject it", role)
		}
	}
}

func TestUserRequiresAClass(t *testing.T) {
	env := NewEnv(t)

	if _, err := env.DB.Exec(
		"INSERT INTO users (username, password_hash, role, class_id) VALUES ('no_class', 'hash', 'student', NULL)",
	); err == nil {
		t.Error("a user with class_id = NULL was accepted")
	}

	if _, err := env.DB.Exec(
		"INSERT INTO users (username, password_hash, role) VALUES ('omitted_class', 'hash', 'student')",
	); err == nil {
		t.Error("a user with class_id omitted was accepted")
	}
}

func TestUserClassMustExist(t *testing.T) {
	env := NewEnv(t)

	if _, err := env.DB.Exec(
		"INSERT INTO users (username, password_hash, role, class_id) VALUES ('ghost', 'hash', 'student', 999999)",
	); err == nil {
		t.Error("a user referencing a missing class was accepted")
	}
}

// AC16: materials.class_id is mandatory, whether NULL or omitted.
func TestMaterialsRequireAClass(t *testing.T) {
	env := NewEnv(t)

	classID := insertClass(t, env.DB, "Class A")
	teacherID := insertUser(t, env.DB, "teacher_a", "teacher", classID)

	if _, err := env.DB.Exec(`
		INSERT INTO materials (class_id, uploaded_by, title, original_filename, stored_filename, content_type)
		VALUES (NULL, ?, 't', 'a.md', 'store-null', 'text/markdown')`, teacherID,
	); err == nil {
		t.Error("materials with class_id = NULL was accepted")
	}

	if _, err := env.DB.Exec(`
		INSERT INTO materials (uploaded_by, title, original_filename, stored_filename, content_type)
		VALUES (?, 't', 'a.md', 'store-omitted', 'text/markdown')`, teacherID,
	); err == nil {
		t.Error("materials with class_id omitted was accepted")
	}

	if count := env.CountRows(t, "materials"); count != 0 {
		t.Errorf("materials = %d, want 0 rejected rows", count)
	}
}

// AC17: knowledge_entries.class_id is mandatory, whether NULL or omitted.
func TestKnowledgeEntriesRequireAClass(t *testing.T) {
	env := NewEnv(t)

	classID := insertClass(t, env.DB, "Class A")
	teacherID := insertUser(t, env.DB, "teacher_a", "teacher", classID)
	materialID := insertMaterial(t, env.DB, classID, teacherID, "store-1")

	if _, err := env.DB.Exec(
		"INSERT INTO knowledge_entries (class_id, material_id, body_text) VALUES (NULL, ?, 'text')", materialID,
	); err == nil {
		t.Error("knowledge_entries with class_id = NULL was accepted")
	}

	if _, err := env.DB.Exec(
		"INSERT INTO knowledge_entries (material_id, body_text) VALUES (?, 'text')", materialID,
	); err == nil {
		t.Error("knowledge_entries with class_id omitted was accepted")
	}

	if count := env.CountRows(t, "knowledge_entries"); count != 0 {
		t.Errorf("knowledge_entries = %d, want 0 rejected rows", count)
	}
}

// DA01: the composite key stops an uploader from another class.
func TestMaterialUploaderMustShareTheClass(t *testing.T) {
	env := NewEnv(t)

	classA := insertClass(t, env.DB, "Class A")
	classB := insertClass(t, env.DB, "Class B")
	studentB := insertUser(t, env.DB, "student_b1", "student", classB)

	if _, err := env.DB.Exec(`
		INSERT INTO materials (class_id, uploaded_by, title, original_filename, stored_filename, content_type)
		VALUES (?, ?, 't', 'a.md', 'store-cross', 'text/markdown')`, classA, studentB,
	); err == nil {
		t.Error("a Class B user uploaded material into Class A, want a foreign-key error")
	}
}

// DA01: a knowledge entry cannot point at another class's material.
func TestKnowledgeEntryMustMatchItsMaterialClass(t *testing.T) {
	env := NewEnv(t)

	classA := insertClass(t, env.DB, "Class A")
	classB := insertClass(t, env.DB, "Class B")
	teacherA := insertUser(t, env.DB, "teacher_a", "teacher", classA)
	materialA := insertMaterial(t, env.DB, classA, teacherA, "store-a")

	if _, err := env.DB.Exec(
		"INSERT INTO knowledge_entries (class_id, material_id, body_text) VALUES (?, ?, 'text')",
		classB, materialA,
	); err == nil {
		t.Error("a Class B knowledge entry referenced Class A material, want a foreign-key error")
	}
}

// One extracted text per material, enforced by the database.
func TestKnowledgeEntryIsUniquePerMaterial(t *testing.T) {
	env := NewEnv(t)

	classID := insertClass(t, env.DB, "Class A")
	teacherID := insertUser(t, env.DB, "teacher_a", "teacher", classID)
	materialID := insertMaterial(t, env.DB, classID, teacherID, "store-a")

	if _, err := env.DB.Exec(
		"INSERT INTO knowledge_entries (class_id, material_id, body_text) VALUES (?, ?, 'first')",
		classID, materialID,
	); err != nil {
		t.Fatalf("first knowledge entry: %v", err)
	}

	if _, err := env.DB.Exec(
		"INSERT INTO knowledge_entries (class_id, material_id, body_text) VALUES (?, ?, 'second')",
		classID, materialID,
	); err == nil {
		t.Error("a second knowledge entry for the same material was accepted")
	}
}

// AU06 / DATA-01: the digest column is 64 ASCII hex characters and unique.
func TestSessionDigestIsUniqueHex(t *testing.T) {
	env := NewEnv(t)

	classID := insertClass(t, env.DB, "Class A")
	userID := insertUser(t, env.DB, "teacher_a", "teacher", classID)

	digest := strings.Repeat("a", 64)

	if _, err := env.DB.Exec(
		"INSERT INTO sessions (session_id, user_id, expires_at) VALUES (?, ?, UTC_TIMESTAMP(6))",
		digest, userID,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	if _, err := env.DB.Exec(
		"INSERT INTO sessions (session_id, user_id, expires_at) VALUES (?, ?, UTC_TIMESTAMP(6))",
		digest, userID,
	); err == nil {
		t.Error("a duplicate session digest was accepted")
	}

	if _, err := env.DB.Exec(
		"INSERT INTO sessions (session_id, user_id, expires_at) VALUES (?, ?, UTC_TIMESTAMP(6))",
		strings.Repeat("a", 65), userID,
	); err == nil {
		t.Error("a 65-character session id was accepted, want CHAR(64) to reject it")
	}

	// DA01: the retired refresh credential has no column to live in.
	var columns int
	if err := env.DB.QueryRow(`
		SELECT COUNT(*)
		  FROM information_schema.columns
		 WHERE table_schema = DATABASE() AND table_name = 'sessions' AND column_name = 'refresh_id'`,
	).Scan(&columns); err != nil {
		t.Fatalf("inspect sessions columns: %v", err)
	}
	if columns != 0 {
		t.Error("sessions still has a refresh_id column")
	}
}

// Timestamps must be usable as UTC DATETIME(6) values.
func TestTimestampsDefaultToUtcMicroseconds(t *testing.T) {
	env := NewEnv(t)

	insertClass(t, env.DB, "Class A")

	var createdAt string
	if err := env.DB.QueryRow(
		"SELECT DATE_FORMAT(created_at, '%Y-%m-%d %H:%i:%s.%f') FROM classes LIMIT 1",
	).Scan(&createdAt); err != nil {
		t.Fatalf("read created_at: %v", err)
	}

	if !strings.Contains(createdAt, ".") {
		t.Errorf("created_at = %q, want microsecond precision", createdAt)
	}
}

func migrateAgain(t *testing.T, env *Env) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return db.Migrate(ctx, env.Config)
}
