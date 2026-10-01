package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"campusclaw/internal/httpx"
)

var ErrNoSession = errors.New("no active session")

type Store struct {
	db *sql.DB
}

type Credentials struct {
	Access    Token
	ExpiresAt time.Time
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Issue creates the single session row for a login. A live token presented by
// the same user is revoked first; the session's absolute expiry is set here and
// nothing can extend it afterwards.
func (s *Store) Issue(ctx context.Context, userID uint64, previousAccess string, now time.Time) (Credentials, error) {
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Credentials{}, fmt.Errorf("begin session transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if previousAccess != "" {
		if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ? AND session_id = ?", userID, DigestToken(previousAccess)); err != nil {
			return Credentials{}, fmt.Errorf("revoke previous access: %w", err)
		}
	}

	access, err := NewToken()
	if err != nil {
		return Credentials{}, fmt.Errorf("generate access token: %w", err)
	}
	expiresAt := now.Add(SessionLifetime).UTC()
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO sessions (session_id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		access.Digest, userID, now.UTC(), expiresAt,
	); err != nil {
		return Credentials{}, fmt.Errorf("create session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Credentials{}, fmt.Errorf("commit session transaction: %w", err)
	}
	return Credentials{Access: access, ExpiresAt: expiresAt}, nil
}

func (s *Store) Resolve(ctx context.Context, token string, now time.Time) (User, error) {
	var (
		userID, classID           uint64
		username, role, className string
	)
	err := s.db.QueryRowContext(ctx,
		"SELECT u.id, u.username, u.role, u.class_id, c.name "+
			"FROM sessions s JOIN users u ON u.id = s.user_id JOIN classes c ON c.id = u.class_id "+
			"WHERE s.session_id = ? AND s.expires_at > ?",
		DigestToken(token), now.UTC(),
	).Scan(&userID, &username, &role, &classID, &className)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNoSession
	}
	if err != nil {
		return User{}, fmt.Errorf("resolve session: %w", err)
	}
	return User{
		ID: httpx.ID(userID), Username: username, Role: role,
		ClassID: httpx.ID(classID), ClassName: className,
	}, nil
}

func (s *Store) Delete(ctx context.Context, token string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE session_id = ?", DigestToken(token)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Store) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", now.UTC())
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count expired sessions: %w", err)
	}
	return deleted, nil
}
