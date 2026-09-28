// Package auth turns a session cookie into a server-side identity and guards
// the routes that depend on one.
package auth

import (
	"context"

	"campusclaw/internal/httpx"
)

// The only two roles the system recognises. The database CHECK constraint
// enforces the same set.
const (
	RoleTeacher = "teacher"
	RoleStudent = "student"
)

// User is the identity restored from the database on every request. Role and
// class are read fresh from users/classes, never taken from the cookie, so a
// revoked or demoted account cannot keep old privileges.
type User struct {
	ID        httpx.ID `json:"id"`
	Username  string   `json:"username"`
	Role      string   `json:"role"`
	ClassID   httpx.ID `json:"class_id"`
	ClassName string   `json:"class_name"`
}

// IsTeacher reports whether the user may upload material.
func (u User) IsTeacher() bool {
	return u.Role == RoleTeacher
}

type userContextKey struct{}

// WithUser attaches the resolved identity to a request context.
func WithUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, userContextKey{}, user)
}

// UserFrom returns the identity attached by RequireUser.
func UserFrom(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userContextKey{}).(User)
	return user, ok
}
