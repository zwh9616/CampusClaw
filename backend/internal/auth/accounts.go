package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"campusclaw/internal/httpx"
)

// ErrNoAccount means no user row matches the requested username.
var ErrNoAccount = errors.New("no such account")

// Account is a user together with the stored password hash. It exists only for
// the login path; the hash never leaves this package.
type Account struct {
	User         User
	PasswordHash string
}

// Accounts reads user rows.
type Accounts struct {
	db *sql.DB
}

// NewAccounts builds an account reader over an existing pool.
func NewAccounts(db *sql.DB) *Accounts {
	return &Accounts{db: db}
}

// ByUsername loads the account to authenticate against. The username column
// uses a binary collation, so the match is case-sensitive.
func (a *Accounts) ByUsername(ctx context.Context, username string) (Account, error) {
	var (
		account  Account
		userID   uint64
		classID  uint64
		role     string
		className string
	)

	err := a.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.role, u.class_id, c.name, u.password_hash
		  FROM users u
		  JOIN classes c ON c.id = u.class_id
		 WHERE u.username = ?`,
		username,
	).Scan(
		&userID,
		&account.User.Username,
		&role,
		&classID,
		&className,
		&account.PasswordHash,
	)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Account{}, ErrNoAccount
	case err != nil:
		return Account{}, fmt.Errorf("look up account: %w", err)
	}

	account.User.ID = httpx.ID(userID)
	account.User.Role = role
	account.User.ClassID = httpx.ID(classID)
	account.User.ClassName = className

	return account, nil
}
