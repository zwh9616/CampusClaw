package tests

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"campusclaw/internal/seed"
)

func seedOnce(t *testing.T, env *Env) seed.Result {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := seed.Run(ctx, env.DB, env.Config)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return result
}

// Seed state as it must survive repeated initialisation.
type seedSnapshot struct {
	classes    int
	users      int
	materials  int
	knowledge  int
	hashes     map[string]string
	classNames map[string]uint64
}

func snapshot(t *testing.T, env *Env) seedSnapshot {
	t.Helper()

	snap := seedSnapshot{
		classes:    env.CountRows(t, "classes"),
		users:      env.CountRows(t, "users"),
		materials:  env.CountRows(t, "materials"),
		knowledge:  env.CountRows(t, "knowledge_entries"),
		hashes:     map[string]string{},
		classNames: map[string]uint64{},
	}

	rows, err := env.DB.Query("SELECT username, password_hash FROM users")
	if err != nil {
		t.Fatalf("read users: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var username, hash string
		if err := rows.Scan(&username, &hash); err != nil {
			t.Fatalf("scan user: %v", err)
		}
		snap.hashes[username] = hash
	}

	classRows, err := env.DB.Query("SELECT id, name FROM classes")
	if err != nil {
		t.Fatalf("read classes: %v", err)
	}
	defer classRows.Close()

	for classRows.Next() {
		var id uint64
		var name string
		if err := classRows.Scan(&id, &name); err != nil {
			t.Fatalf("scan class: %v", err)
		}
		snap.classNames[name] = id
	}

	return snap
}

func TestSeedCreatesTheAgreedAccounts(t *testing.T) {
	env := NewEnv(t)

	result := seedOnce(t, env)

	if result.CreatedClasses != 2 {
		t.Errorf("created %d classes, want 2", result.CreatedClasses)
	}
	if result.CreatedUsers != 4 {
		t.Errorf("created %d accounts, want 4", result.CreatedUsers)
	}

	expected := map[string]struct {
		role    string
		class   string
		student bool
	}{
		"teacher_a":  {role: "teacher", class: "Class A"},
		"student_a1": {role: "student", class: "Class A"},
		"teacher_b":  {role: "teacher", class: "Class B"},
		"student_b1": {role: "student", class: "Class B"},
	}

	for username, want := range expected {
		var role, className string
		err := env.DB.QueryRow(`
			SELECT u.role, c.name
			  FROM users u
			  JOIN classes c ON c.id = u.class_id
			 WHERE u.username = ?`, username).Scan(&role, &className)

		if err != nil {
			t.Errorf("%s: %v", username, err)
			continue
		}

		if role != want.role {
			t.Errorf("%s role = %q, want %q", username, role, want.role)
		}
		if className != want.class {
			t.Errorf("%s class = %q, want %q", username, className, want.class)
		}
	}
}

// DA02: the stored hash is bcrypt of the environment password, never plaintext.
func TestSeedHashesPasswordsWithBcryptFromEnvironment(t *testing.T) {
	env := NewEnv(t)
	seedOnce(t, env)

	for _, username := range []string{"teacher_a", "student_a1", "teacher_b", "student_b1"} {
		var hash string
		if err := env.DB.QueryRow("SELECT password_hash FROM users WHERE username = ?", username).Scan(&hash); err != nil {
			t.Fatalf("%s: %v", username, err)
		}

		password := env.Password(t, username)

		if hash == password {
			t.Errorf("%s: password_hash is the plaintext password", username)
		}

		if !strings.HasPrefix(hash, "$2a$12$") && !strings.HasPrefix(hash, "$2b$12$") {
			t.Errorf("%s: hash %q is not a cost-12 bcrypt hash", username, hash)
		}

		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
			t.Errorf("%s: bcrypt comparison against the environment password failed: %v", username, err)
		}

		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password+"x")); err == nil {
			t.Errorf("%s: a wrong password verified successfully", username)
		}
	}
}

// AC20: re-running the seed must not duplicate, reset or delete anything.
func TestSeedIsIdempotentAndPreservesData(t *testing.T) {
	env := NewEnv(t)
	seedOnce(t, env)

	// Both classes keep their material and original bytes across seed passes.
	uploadMarkdown(t, env, env.Login(t, "teacher_a", env.Password(t, "teacher_a")), "A stays", "Class A original")
	uploadMarkdown(t, env, env.Login(t, "teacher_b", env.Password(t, "teacher_b")), "B stays", "Class B original")
	filesBefore := storedFiles(t, env.Config.UploadDir)
	bytesBefore := make(map[string][]byte, len(filesBefore))
	for _, path := range filesBefore {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read original %s: %v", path, err)
		}
		bytesBefore[path] = data
	}

	before := snapshot(t, env)

	for i := 0; i < 2; i++ {
		result := seedOnce(t, env)

		if result.CreatedClasses != 0 || result.CreatedUsers != 0 {
			t.Errorf("pass %d created %d classes and %d accounts, want none",
				i+1, result.CreatedClasses, result.CreatedUsers)
		}
	}

	after := snapshot(t, env)

	if before.classes != after.classes || before.users != after.users {
		t.Errorf("counts changed: classes %d->%d, users %d->%d",
			before.classes, after.classes, before.users, after.users)
	}

	if before.materials != after.materials || before.knowledge != after.knowledge {
		t.Errorf("material changed: materials %d->%d, knowledge %d->%d",
			before.materials, after.materials, before.knowledge, after.knowledge)
	}

	for username, hash := range before.hashes {
		if after.hashes[username] != hash {
			t.Errorf("%s: password hash changed across seeds", username)
		}
	}

	filesAfter := storedFiles(t, env.Config.UploadDir)
	if !reflect.DeepEqual(filesBefore, filesAfter) {
		t.Errorf("stored originals changed: before %v, after %v", filesBefore, filesAfter)
	}
	for _, path := range filesBefore {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read original after seed %s: %v", path, err)
			continue
		}
		if !bytes.Equal(data, bytesBefore[path]) {
			t.Errorf("original %s changed across seeds", path)
		}
	}

	for _, username := range []string{"teacher_a", "student_a1", "teacher_b", "student_b1"} {
		if cookie := env.Login(t, username, env.Password(t, username)); cookie == nil {
			t.Errorf("%s: original credentials no longer work", username)
		}
	}

}

// A pre-existing account that contradicts the expected role or class must stop
// the seed rather than be silently rewritten.
func TestSeedRejectsContradictoryExistingAccount(t *testing.T) {
	cases := map[string]struct {
		role  string
		class string
	}{
		"wrong role":  {role: "student", class: "Class A"},
		"wrong class": {role: "teacher", class: "Class B"},
	}

	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			env := NewEnv(t)

			classID := insertClass(t, env.DB, setup.class)
			insertUser(t, env.DB, "teacher_a", setup.role, classID)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if _, err := seed.Run(ctx, env.DB, env.Config); err == nil {
				t.Error("seed succeeded despite a contradictory pre-existing account")
			}

			// The existing row must be left exactly as it was.
			var role string
			if err := env.DB.QueryRow("SELECT role FROM users WHERE username = 'teacher_a'").Scan(&role); err != nil {
				t.Fatalf("read role: %v", err)
			}
			if role != setup.role {
				t.Errorf("role = %q, want it untouched at %q", role, setup.role)
			}
		})
	}
}

// A failed seed must not leave a half-created class behind.
func TestSeedRollsBackWhenItFails(t *testing.T) {
	env := NewEnv(t)

	classID := insertClass(t, env.DB, "Class B")
	insertUser(t, env.DB, "teacher_a", "student", classID)

	classesBefore := env.CountRows(t, "classes")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := seed.Run(ctx, env.DB, env.Config); err == nil {
		t.Fatal("seed succeeded, want an error")
	}

	if after := env.CountRows(t, "classes"); after != classesBefore {
		t.Errorf("classes = %d, want %d: the transaction rolled back partially", after, classesBefore)
	}
}

// A pre-existing Teacher B identity must never be silently reassigned.
func TestSeedRejectsContradictoryTeacherB(t *testing.T) {
	for name, setup := range map[string]struct {
		role  string
		class string
	}{
		"wrong role":  {role: "student", class: "Class B"},
		"wrong class": {role: "teacher", class: "Class A"},
	} {
		t.Run(name, func(t *testing.T) {
			env := NewEnv(t)
			classID := insertClass(t, env.DB, setup.class)
			insertUser(t, env.DB, "teacher_b", setup.role, classID)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, err := seed.Run(ctx, env.DB, env.Config); err == nil {
				t.Fatal("seed accepted contradictory teacher_b identity")
			}

			var role, className string
			err := env.DB.QueryRow(`SELECT u.role, c.name FROM users u JOIN classes c ON c.id = u.class_id WHERE u.username = 'teacher_b'`).Scan(&role, &className)
			if err != nil {
				t.Fatalf("read teacher_b after failed seed: %v", err)
			}
			if role != setup.role || className != setup.class {
				t.Errorf("teacher_b changed to %s/%s, want %s/%s", role, className, setup.role, setup.class)
			}
		})
	}
}
