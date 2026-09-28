// Package tests holds the database-backed integration suite. It talks to a
// real MySQL server so that NOT NULL, CHECK, foreign key and UNIQUE behaviour
// is exercised for real rather than approximated by SQLite.
package tests

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"campusclaw/internal/config"
	"campusclaw/internal/db"
	"campusclaw/internal/materials"
	"campusclaw/internal/seed"
	"campusclaw/internal/server"
)

// testDatabaseSuffix guards against a misconfigured MYSQL_DATABASE pointing at
// real data: the suite only ever resets a database it recognises as a test one.
const testDatabaseSuffix = "_test"

// businessTables are dropped between tests, in foreign-key-safe order.
var businessTables = []string{
	"knowledge_entries",
	"materials",
	"sessions",
	"users",
	"classes",
	"schema_migrations",
}

// Env is a migrated database plus the configuration that produced it.
type Env struct {
	Config *config.Config
	DB     *sql.DB
}

// NewEnv returns a freshly migrated, empty database.
//
// It skips when no database is configured, so `go test ./...` stays runnable
// without one. Every secret it needs is generated at run time; nothing is read
// from the repository.
func NewEnv(t *testing.T) *Env {
	t.Helper()

	if os.Getenv("MYSQL_HOST") == "" {
		t.Skip("MYSQL_HOST is not set: run this suite against the isolated test database")
	}

	fillMissingSecrets(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if !strings.HasSuffix(cfg.MySQL.Name, testDatabaseSuffix) {
		t.Fatalf("refusing to run against database %q: test databases must end in %q",
			cfg.MySQL.Name, testDatabaseSuffix)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := db.Migrate(ctx, cfg); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handle, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	t.Cleanup(func() { handle.Close() })

	resetTables(t, handle)

	// Re-create the schema now that the tables are gone.
	if err := db.Migrate(ctx, cfg); err != nil {
		t.Fatalf("migrate after reset: %v", err)
	}

	return &Env{Config: cfg, DB: handle}
}

// Seed creates the two classes and three accounts.
func (e *Env) Seed(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := seed.Run(ctx, e.DB, e.Config); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// Server returns the fully wired API handler for this environment.
func (e *Env) Server() http.Handler {
	return server.New(e.Config, e.DB)
}

// ServerWith returns the handler with extra server options applied, so a test
// can substitute a dependency it needs to control.
func (e *Env) ServerWith(options ...server.Option) http.Handler {
	return server.New(e.Config, e.DB, options...)
}

// Do sends a request through the real handler and returns the recorder.
func (e *Env) Do(t *testing.T, request *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	e.Server().ServeHTTP(recorder, request)
	return recorder
}

// Login authenticates an account as a browser with no existing session.
func (e *Env) Login(t *testing.T, username, password string) *http.Cookie {
	t.Helper()
	return e.LoginWith(t, username, password, nil)
}

// LoginWith authenticates an account while presenting an existing cookie, the
// way a browser re-logging in from a page that already has a session does. The
// server uses that cookie to revoke the superseded session.
func (e *Env) LoginWith(t *testing.T, username, password string, existing *http.Cookie) *http.Cookie {
	t.Helper()

	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if existing != nil {
		request.AddCookie(existing)
	}

	recorder := e.Do(t, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login %s: status = %d, body = %s", username, recorder.Code, recorder.Body.String())
	}

	cookies := recorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("login %s: no session cookie returned", username)
	}

	return cookies[0]
}

// Password returns the generated seed password for an account.
func (e *Env) Password(t *testing.T, username string) string {
	t.Helper()

	switch username {
	case "teacher_a":
		return e.Config.Seed.TeacherAPassword
	case "student_a1":
		return e.Config.Seed.StudentA1Password
	case "student_b1":
		return e.Config.Seed.StudentB1Password
	default:
		t.Fatalf("unknown seed account %q", username)
		return ""
	}
}

// UserID looks up a seeded account's id.
func (e *Env) UserID(t *testing.T, username string) uint64 {
	t.Helper()

	var id uint64
	if err := e.DB.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&id); err != nil {
		t.Fatalf("look up user %q: %v", username, err)
	}
	return id
}

// ClassID looks up a class id by name.
func (e *Env) ClassID(t *testing.T, name string) uint64 {
	t.Helper()

	var id uint64
	if err := e.DB.QueryRow("SELECT id FROM classes WHERE name = ?", name).Scan(&id); err != nil {
		t.Fatalf("look up class %q: %v", name, err)
	}
	return id
}

// CountRows returns the row count of a table.
func (e *Env) CountRows(t *testing.T, table string) int {
	t.Helper()

	if !allowedTable(table) {
		t.Fatalf("refusing to count unknown table %q", table)
	}

	var count int
	if err := e.DB.QueryRow("SELECT COUNT(*) FROM `" + table + "`").Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func allowedTable(table string) bool {
	for _, known := range businessTables {
		if known == table {
			return true
		}
	}
	return false
}

// resetTables empties the schema without touching any other database.
func resetTables(t *testing.T, handle *sql.DB) {
	t.Helper()

	if _, err := handle.Exec("SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		t.Fatalf("disable foreign key checks: %v", err)
	}

	for _, table := range businessTables {
		if _, err := handle.Exec("DROP TABLE IF EXISTS `" + table + "`"); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}

	if _, err := handle.Exec("SET FOREIGN_KEY_CHECKS = 1"); err != nil {
		t.Fatalf("enable foreign key checks: %v", err)
	}
}

// fillMissingSecrets supplies any variable the harness did not set, using
// freshly generated values.
func fillMissingSecrets(t *testing.T) {
	t.Helper()

	for _, name := range []string{
		"SEED_TEACHER_A_PASSWORD",
		"SEED_STUDENT_A1_PASSWORD",
		"SEED_STUDENT_B1_PASSWORD",
	} {
		if os.Getenv(name) == "" {
			t.Setenv(name, randomSecret(t))
		}
	}

	if os.Getenv("PUBLIC_ORIGIN") == "" {
		t.Setenv("PUBLIC_ORIGIN", "http://localhost:8080")
	}

	if os.Getenv("SESSION_COOKIE_SECURE") == "" {
		t.Setenv("SESSION_COOKIE_SECURE", "false")
	}

	// Each test gets its own upload root, so stored originals from one test
	// can never be observed by another.
	if os.Getenv("UPLOAD_DIR") == "" {
		t.Setenv("UPLOAD_DIR", t.TempDir())
	}
}

// assertRowCount counts a table over a brand new connection.
//
// A separate handle matters after a rollback test: it cannot be served by the
// handler's pool, so an uncommitted or rolled-back row cannot be masked by a
// connection that still holds an old snapshot.
func assertRowCount(t *testing.T, env *Env, table string, want int) {
	t.Helper()

	if !allowedTable(table) {
		t.Fatalf("refusing to count unknown table %q", table)
	}

	handle, err := sql.Open("mysql", env.Config.MySQL.DSN())
	if err != nil {
		t.Fatalf("open verification connection: %v", err)
	}
	defer handle.Close()

	if err := handle.Ping(); err != nil {
		t.Fatalf("ping verification connection: %v", err)
	}

	var count int
	if err := handle.QueryRow("SELECT COUNT(*) FROM `" + table + "`").Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}

	if count != want {
		t.Errorf("%s = %d, want %d", table, count, want)
	}
}

// assertNoRows asserts a table is empty.
func assertNoRows(t *testing.T, env *Env, table string) {
	t.Helper()
	assertRowCount(t, env, table, 0)
}

// countFilesUnder counts regular files beneath root, treating a missing root
// as empty so "no uploads yet" is not an error.
func countFilesUnder(t *testing.T, root string) int {
	t.Helper()

	count := 0

	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	return count
}

// storedFiles returns every stored original beneath root.
func storedFiles(t *testing.T, root string) []string {
	t.Helper()

	var found []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !entry.IsDir() {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	return found
}

// writeFixture stores fixture bytes on disk and returns the path.
func writeFixture(t *testing.T, dir, name string, content []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}

	return path
}

// newPDFExtractorForTest builds the extractor exactly as the API wires it.
func newPDFExtractorForTest() materials.PDFExtractor {
	return materials.NewPDFExtractor()
}

func randomSecret(t *testing.T) string {
	t.Helper()

	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("generate test secret: %v", err)
	}
	return hex.EncodeToString(raw)
}
