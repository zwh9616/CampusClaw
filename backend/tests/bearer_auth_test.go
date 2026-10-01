package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"campusclaw/internal/auth"
)

// The whole session lifecycle over one opaque token: a single hashed row, the
// Authorization header as the only credential, re-login revoking the token the
// client presented, and logout deleting the row.
func TestBearerSessionLifecycle(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	first := env.Login(t, "teacher_a", env.Password(t, "teacher_a"))

	if got := env.get(t, "/api/me", first); got.Code != http.StatusOK {
		t.Fatalf("Bearer denied: %d %s", got.Code, got.Body.String())
	}

	// AU07: the same value presented as a cookie is not a credential.
	cookieOnly := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	cookieOnly.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: first.Value})
	cookieOnly.AddCookie(&http.Cookie{Name: "campusclaw_refresh", Value: first.Value})
	if got := env.Do(t, cookieOnly); got.Code != http.StatusUnauthorized || got.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Errorf("cookie-only request = %d, challenge %q", got.Code, got.Header().Get("WWW-Authenticate"))
	}

	// AU06: exactly one row, holding only the digest of the presented token.
	var digest string
	if err := env.DB.QueryRow("SELECT session_id FROM sessions").Scan(&digest); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(first.Value))
	if digest != hex.EncodeToString(hash[:]) || digest == first.Value {
		t.Error("session row does not hold the token digest")
	}

	// AU02: logging in again revokes the token the client just presented.
	second := env.LoginWith(t, "teacher_a", env.Password(t, "teacher_a"), first)
	if second.Value == first.Value {
		t.Fatal("a second login reused the same token")
	}
	if got := env.get(t, "/api/me", first); got.Code != http.StatusUnauthorized {
		t.Errorf("the replaced token still works: %d", got.Code)
	}
	if got := env.get(t, "/api/me", second); got.Code != http.StatusOK {
		t.Errorf("the new token was rejected: %d", got.Code)
	}

	// Nothing extends the fixed lifetime: a login cannot mint a longer session.
	var expiresAt time.Time
	if err := env.DB.QueryRow("SELECT expires_at FROM sessions").Scan(&expiresAt); err != nil {
		t.Fatal(err)
	}
	if expiresAt.Before(time.Now()) || expiresAt.After(time.Now().Add(auth.SessionLifetime+time.Minute)) {
		t.Errorf("session expiry %s is outside the fixed lifetime", expiresAt)
	}

	// AC19: a cookie cannot drive logout, and rejecting it must not revoke the
	// session the Bearer token still owns.
	cookieLogout := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	cookieLogout.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: second.Value})
	if got := env.Do(t, cookieLogout); got.Code != http.StatusUnauthorized {
		t.Errorf("cookie authorized logout: %d", got.Code)
	}
	if got := env.get(t, "/api/me", second); got.Code != http.StatusOK {
		t.Errorf("a rejected cookie logout revoked the session: %d", got.Code)
	}

	logout := env.postJSON(t, "/api/logout", "", second, nil)
	if logout.Code != http.StatusNoContent {
		t.Errorf("logout = %d", logout.Code)
	}
	if cookies := logout.Result().Cookies(); len(cookies) != 0 {
		t.Errorf("logout set %d cookies, want none", len(cookies))
	}
	if count := env.CountRows(t, "sessions"); count != 0 {
		t.Errorf("sessions = %d, want 0 after logout", count)
	}
	if got := env.get(t, "/api/me", second); got.Code != http.StatusUnauthorized {
		t.Errorf("the token survived logout: %d", got.Code)
	}
}

// An expired session must not authenticate even before a cleanup pass runs.
func TestExpiredBearerTokenIsRejected(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	token := env.Login(t, "student_a1", env.Password(t, "student_a1"))
	if _, err := env.DB.Exec("UPDATE sessions SET expires_at = ? WHERE session_id = ?",
		time.Now().UTC().Add(-time.Second), auth.DigestToken(token.Value)); err != nil {
		t.Fatal(err)
	}

	// The row is deliberately left in place.
	if got := env.get(t, "/api/me", token); got.Code != http.StatusUnauthorized {
		t.Errorf("expired token = %d, want 401", got.Code)
	}
}

func TestBearerRejectsMalformedAndDuplicateHeaders(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	access := env.Login(t, "student_a1", env.Password(t, "student_a1")).Value

	cases := [][]string{
		{"Basic " + access},
		{"Bearer  " + access},
		{"Bearer " + access, "Bearer " + access},
	}
	for _, headers := range cases {
		request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		for _, header := range headers {
			request.Header.Add("Authorization", header)
		}
		response := env.Do(t, request)
		if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Errorf("headers %v: status = %d", headers, response.Code)
		}
	}
}

// 0004 must be additive. 0003 shipped with a refresh_id column that is NOT NULL
// and has no default, so simply deleting it from the source would leave every
// insert failing on any database that already recorded it. This rewinds a
// database to that shape and checks the new migration clears the pre-upgrade
// rows and retires the column.
func TestMigrationRetiresTheRefreshColumn(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	if _, err := env.DB.Exec(
		"ALTER TABLE sessions ADD COLUMN refresh_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL AFTER session_id, ADD UNIQUE KEY uq_sessions_refresh_id (refresh_id)",
	); err != nil {
		t.Fatalf("restore refresh_id: %v", err)
	}
	if _, err := env.DB.Exec("DELETE FROM schema_migrations WHERE version = ?", "0004_drop_refresh_id.sql"); err != nil {
		t.Fatalf("unrecord 0004: %v", err)
	}
	if _, err := env.DB.Exec(
		"INSERT INTO sessions (session_id, refresh_id, user_id, expires_at) VALUES (?, ?, ?, ?)",
		auth.DigestToken("old-cookie"), auth.DigestToken("old-refresh"),
		env.UserID(t, "teacher_a"), time.Now().UTC().Add(time.Hour),
	); err != nil {
		t.Fatalf("insert pre-upgrade session: %v", err)
	}

	if err := migrateAgain(t, env); err != nil {
		t.Fatalf("apply 0004: %v", err)
	}

	if got := env.CountRows(t, "sessions"); got != 0 {
		t.Errorf("sessions after migration = %d, want the pre-upgrade rows cleared", got)
	}

	var columns int
	if err := env.DB.QueryRow(`
		SELECT COUNT(*)
		  FROM information_schema.columns
		 WHERE table_schema = DATABASE() AND table_name = 'sessions' AND column_name = 'refresh_id'`,
	).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != 0 {
		t.Error("refresh_id survived the migration")
	}

	// A second pass is a no-op, and inserts work again now the column is gone.
	if err := migrateAgain(t, env); err != nil {
		t.Errorf("second 0004 pass: %v", err)
	}
	token := env.Login(t, "teacher_a", env.Password(t, "teacher_a"))
	if got := env.get(t, "/api/me", token); got.Code != http.StatusOK {
		t.Errorf("login after migration = %d", got.Code)
	}
}
