// Package seed creates the two classes and four accounts the application
// starts from. It is idempotent: re-running never rewrites an existing account
// and never touches teaching material.
package seed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"campusclaw/internal/config"
)

// bcryptCost matches design D2; it is deliberately not configurable.
const bcryptCost = 12

const (
	classAName = "Class A"
	classBName = "Class B"

	roleTeacher = "teacher"
	roleStudent = "student"
)

// Result counts what a single seed pass created, so start-up can log progress
// without logging anything sensitive.
type Result struct {
	CreatedClasses int
	CreatedUsers   int
}

type account struct {
	username string
	role     string
	class    string
	password string
}

// Run creates any missing classes and accounts in one transaction.
//
// Existing rows are authoritative: an account that already exists keeps its
// password hash, role and class untouched. If an existing row contradicts the
// expected role or class the run fails instead of silently rewriting it, which
// would be indistinguishable from tampering.
func Run(ctx context.Context, db *sql.DB, cfg *config.Config) (Result, error) {
	accounts := []account{
		{username: "teacher_a", role: roleTeacher, class: classAName, password: cfg.Seed.TeacherAPassword},
		{username: "student_a1", role: roleStudent, class: classAName, password: cfg.Seed.StudentA1Password},
		{username: "teacher_b", role: roleTeacher, class: classBName, password: cfg.Seed.TeacherBPassword},
		{username: "student_b1", role: roleStudent, class: classBName, password: cfg.Seed.StudentB1Password},
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, fmt.Errorf("begin seed transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once the transaction is committed

	var result Result

	classIDs := make(map[string]uint64, 2)

	for _, name := range []string{classAName, classBName} {
		id, created, err := ensureClass(ctx, tx, name)
		if err != nil {
			return Result{}, err
		}
		classIDs[name] = id
		if created {
			result.CreatedClasses++
		}
	}

	for _, account := range accounts {
		created, err := ensureAccount(ctx, tx, account, classIDs[account.class])
		if err != nil {
			return Result{}, err
		}
		if created {
			result.CreatedUsers++
		}
	}

	if err := tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("commit seed transaction: %w", err)
	}

	return result, nil
}

func ensureClass(ctx context.Context, tx *sql.Tx, name string) (uint64, bool, error) {
	var id uint64

	err := tx.QueryRowContext(ctx, "SELECT id FROM classes WHERE name = ?", name).Scan(&id)
	switch {
	case err == nil:
		return id, false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return 0, false, fmt.Errorf("look up class %q: %w", name, err)
	}

	inserted, err := tx.ExecContext(ctx, "INSERT INTO classes (name) VALUES (?)", name)
	if err != nil {
		return 0, false, fmt.Errorf("create class %q: %w", name, err)
	}

	newID, err := inserted.LastInsertId()
	if err != nil {
		return 0, false, fmt.Errorf("read new class id: %w", err)
	}

	return uint64(newID), true, nil
}

func ensureAccount(ctx context.Context, tx *sql.Tx, account account, classID uint64) (bool, error) {
	var (
		existingID      uint64
		existingRole    string
		existingClassID uint64
	)

	err := tx.QueryRowContext(
		ctx,
		"SELECT id, role, class_id FROM users WHERE username = ?",
		account.username,
	).Scan(&existingID, &existingRole, &existingClassID)

	switch {
	case err == nil:
		if existingRole != account.role || existingClassID != classID {
			return false, fmt.Errorf(
				"seeded account %q already exists as role=%q class_id=%d, expected role=%q class_id=%d",
				account.username, existingRole, existingClassID, account.role, classID,
			)
		}
		return false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return false, fmt.Errorf("look up account %q: %w", account.username, err)
	}

	// Only a brand-new account hashes its password; existing hashes are never
	// recomputed, which is what keeps a repeated seed from resetting logins.
	hash, err := bcrypt.GenerateFromPassword([]byte(account.password), bcryptCost)
	if err != nil {
		return false, fmt.Errorf("hash password for %q: %w", account.username, err)
	}

	if _, err := tx.ExecContext(
		ctx,
		"INSERT INTO users (username, password_hash, role, class_id) VALUES (?, ?, ?, ?)",
		account.username, string(hash), account.role, classID,
	); err != nil {
		return false, fmt.Errorf("create account %q: %w", account.username, err)
	}

	return true, nil
}
