package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"campusclaw/internal/httpx"
)

// ErrNoSession means no live session matches the presented token. Unknown,
// revoked and expired tokens all produce this same error so no caller can
// distinguish them.
var ErrNoSession = errors.New("no active session")

// Store reads and writes the sessions table.
type Store struct {
	db *sql.DB
}

// NewStore builds a session store over an existing pool.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Rotate issues a new session for userID and deletes the session the browser
// presented, in one transaction, so a login can never leave two live sessions
// for the same browser. Logging in without a previous session is normal.
func (s *Store) Rotate(ctx context.Context, userID uint64, previousToken string, now time.Time) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin session transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	if previousToken != "" {
		if _, err := tx.ExecContext(
			ctx,
			"DELETE FROM sessions WHERE session_id = ?",
			DigestToken(previousToken),
		); err != nil {
			return "", fmt.Errorf("revoke previous session: %w", err)
		}
	}

	token, err := NewToken()
	if err != nil {
		return "", err
	}

	if _, err := tx.ExecContext(
		ctx,
		"INSERT INTO sessions (session_id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		token.Digest, userID, now.UTC(), now.Add(SessionLifetime).UTC(),
	); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit session transaction: %w", err)
	}

	return token.Value, nil
}

// Resolve recovers the current user from a raw cookie token. Role and class
// come from the current database rows, so role changes take effect on the next
// request rather than at the next login.
func (s *Store) Resolve(ctx context.Context, token string, now time.Time) (User, error) {
	var (
		userID    uint64
		username  string
		role      string
		classID   uint64
		className string
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.role, u.class_id, c.name
		  FROM sessions s
		  JOIN users u ON u.id = s.user_id
		  JOIN classes c ON c.id = u.class_id
		 WHERE s.session_id = ? AND s.expires_at > ?`,
		DigestToken(token), now.UTC(),
	).Scan(&userID, &username, &role, &classID, &className)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return User{}, ErrNoSession
	case err != nil:
		return User{}, fmt.Errorf("resolve session: %w", err)
	}

	return User{
		ID:        httpx.ID(userID),
		Username:  username,
		Role:      role,
		ClassID:   httpx.ID(classID),
		ClassName: className,
	}, nil
}

// Delete revokes a single session, the server-side half of logging out.
func (s *Store) Delete(ctx context.Context, token string) error {
	if _, err := s.db.ExecContext(
		ctx,
		"DELETE FROM sessions WHERE session_id = ?",
		DigestToken(token),
	); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpired prunes rows that can no longer authenticate anyone. Expiry is
// enforced by the comparison in Resolve regardless, so a delayed cleanup never
// extends a session's usable life.
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
