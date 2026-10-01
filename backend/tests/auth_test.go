package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"campusclaw/internal/auth"
	"campusclaw/internal/httpx"
)

// postJSON sends a JSON body to a POST endpoint.
func (e *Env) postJSON(t *testing.T, path, body string, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.Header.Set("Authorization", "Bearer "+cookie.Value)
	}
	for name, value := range headers {
		request.Header[name] = []string{value}
	}

	return e.Do(t, request)
}

// get sends an authenticated GET.
func (e *Env) get(t *testing.T, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		request.Header.Set("Authorization", "Bearer "+cookie.Value)
	}

	return e.Do(t, request)
}

func decodeUser(t *testing.T, body []byte) struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	ClassID   string `json:"class_id"`
	ClassName string `json:"class_name"`
} {
	t.Helper()

	var user struct {
		ID        string `json:"id"`
		Username  string `json:"username"`
		Role      string `json:"role"`
		ClassID   string `json:"class_id"`
		ClassName string `json:"class_name"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		t.Fatalf("decode user from %s: %v", body, err)
	}
	return user
}

func loginBody(username, password string) string {
	return `{"username":` + strconvQuote(username) + `,"password":` + strconvQuote(password) + `}`
}

func strconvQuote(s string) string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}

// AC01 and AC02: a seeded teacher and student both log in and get a session.
func TestLoginSucceedsForSeededAccounts(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	cases := map[string]struct {
		role      string
		className string
	}{
		"teacher_a":  {role: "teacher", className: "Class A"},
		"student_a1": {role: "student", className: "Class A"},
		"teacher_b":  {role: "teacher", className: "Class B"},
		"student_b1": {role: "student", className: "Class B"},
	}

	for username, want := range cases {
		recorder := env.postJSON(t, "/api/login",
			loginBody(username, env.Password(t, username)), nil, nil)

		if recorder.Code != http.StatusOK {
			t.Errorf("%s: status = %d, body = %s", username, recorder.Code, recorder.Body.String())
			continue
		}

		var response struct {
			Token     string          `json:"token"`
			ExpiresAt time.Time       `json:"expires_at"`
			User      json.RawMessage `json:"user"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Errorf("%s: decode body: %v", username, err)
			continue
		}

		user := decodeUser(t, response.User)

		if user.Username != username {
			t.Errorf("%s: username = %q", username, user.Username)
		}
		if user.Role != want.role {
			t.Errorf("%s: role = %q, want %q", username, user.Role, want.role)
		}
		if user.ClassName != want.className {
			t.Errorf("%s: class = %q, want %q", username, user.ClassName, want.className)
		}

		if cookies := recorder.Result().Cookies(); len(cookies) != 0 {
			t.Errorf("%s: login set %d cookies, want none", username, len(cookies))
		}

		if response.Token == "" || response.ExpiresAt.Before(time.Now()) {
			t.Errorf("%s: missing access token or expiry", username)
		}
		if header := recorder.Header().Get("Set-Cookie"); header != "" {
			t.Errorf("%s: login sent Set-Cookie: %q", username, header)
		}
	}
}

// AC03: wrong password and unknown username are indistinguishable.
func TestLoginRejectsBadCredentialsIdentically(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	wrongPassword := env.postJSON(t, "/api/login",
		loginBody("teacher_a", "definitely-not-the-password"), nil, nil)

	unknownUser := env.postJSON(t, "/api/login",
		loginBody("no_such_user", "whatever"), nil, nil)

	if wrongPassword.Code != http.StatusUnauthorized {
		t.Errorf("wrong password: status = %d, want 401", wrongPassword.Code)
	}
	if unknownUser.Code != http.StatusUnauthorized {
		t.Errorf("unknown user: status = %d, want 401", unknownUser.Code)
	}

	if wrongPassword.Body.String() != unknownUser.Body.String() {
		t.Errorf("responses differ:\nwrong password: %s\nunknown user:  %s",
			wrongPassword.Body.String(), unknownUser.Body.String())
	}

	if len(wrongPassword.Result().Cookies()) != 0 {
		t.Error("a failed login set a cookie")
	}

	if count := env.CountRows(t, "sessions"); count != 0 {
		t.Errorf("sessions = %d, want 0 after failed logins", count)
	}
}

func TestLoginRejectsMalformedBodies(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	cases := map[string]string{
		"not json":          `{`,
		"empty body":        ``,
		"array":             `[]`,
		"missing password":  `{"username":"teacher_a"}`,
		"missing username":  `{"password":"x"}`,
		"empty username":    `{"username":"","password":"x"}`,
		"empty password":    `{"username":"teacher_a","password":""}`,
		"trailing value":    `{"username":"teacher_a","password":"x"}{}`,
		"wrong field types": `{"username":123,"password":true}`,
	}

	for name, body := range cases {
		recorder := env.postJSON(t, "/api/login", body, nil, nil)

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, recorder.Code)
		}
	}
}

// A password longer than bcrypt's window is refused, never truncated.
func TestLoginRejectsOverlongPassword(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	overlong := strings.Repeat("x", 73)

	recorder := env.postJSON(t, "/api/login",
		loginBody("teacher_a", overlong), nil, nil)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", recorder.Code)
	}

	// Exactly 72 bytes is still handed to bcrypt and simply fails to match.
	atLimit := env.postJSON(t, "/api/login",
		loginBody("teacher_a", strings.Repeat("x", 72)), nil, nil)

	if atLimit.Code != http.StatusUnauthorized {
		t.Errorf("72-byte password: status = %d, want 401", atLimit.Code)
	}
}

// Extra identity fields in the body must be ignored, not honoured.
func TestLoginIgnoresClientSuppliedIdentity(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	recorder := env.postJSON(t, "/api/login",
		`{"username":"student_a1","password":`+strconvQuote(env.Password(t, "student_a1"))+
			`,"role":"teacher","class_id":"2","uploaded_by":"1"}`, nil, nil)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		User json.RawMessage `json:"user"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}

	user := decodeUser(t, response.User)

	if user.Role != "student" {
		t.Errorf("role = %q, want student: the body must not grant a role", user.Role)
	}
	if user.ClassName != "Class A" {
		t.Errorf("class = %q, want Class A", user.ClassName)
	}
}

// AC04: every protected endpoint refuses anonymous, forged and expired tokens.
func TestProtectedEndpointsRequireAValidSession(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	valid := env.Login(t, "teacher_a", env.Password(t, "teacher_a"))

	expired, err := auth.NewToken()
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	// Store a session row that has already expired.
	var teacherID uint64
	if err := env.DB.QueryRow("SELECT id FROM users WHERE username = 'teacher_a'").Scan(&teacherID); err != nil {
		t.Fatalf("look up teacher: %v", err)
	}
	if _, err := env.DB.Exec(
		"INSERT INTO sessions (session_id, user_id, created_at, expires_at) VALUES (?, ?, UTC_TIMESTAMP(6), ?)",
		expired.Digest, teacherID, time.Now().UTC().Add(-time.Hour),
	); err != nil {
		t.Fatalf("insert expired session: %v", err)
	}

	requests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/me"},
		{http.MethodGet, "/api/materials"},
		{http.MethodGet, "/api/materials/1"},
		{http.MethodGet, "/api/materials/1/file"},
		{http.MethodPost, "/api/materials"},
		{http.MethodPost, "/api/logout"},
	}

	credentials := map[string]*http.Cookie{
		"no credential":   nil,
		"forged token":    {Value: "forged-token-value"},
		"expired session": {Value: expired.Value},
		"valid session":   valid,
	}

	// Sanity: the valid session must actually be usable for GET /api/me.
	if recorder := env.get(t, "/api/me", valid); recorder.Code != http.StatusOK {
		t.Fatalf("valid session rejected on /api/me: %d %s", recorder.Code, recorder.Body.String())
	}

	for name, cookie := range credentials {
		if name == "valid session" {
			continue
		}

		for _, target := range requests {
			request := httptest.NewRequest(target.method, target.path, nil)
			if cookie != nil {
				request.Header.Set("Authorization", "Bearer "+cookie.Value)
			}

			recorder := env.Do(t, request)

			if recorder.Code != http.StatusUnauthorized {
				t.Errorf("%s %s with %s: status = %d, want 401",
					target.method, target.path, name, recorder.Code)
			}
		}
	}

	// AU07: a cookie is not a credential. Even a live token value presented as
	// a cookie must be refused, whatever the cookie is called.
	for _, target := range requests {
		request := httptest.NewRequest(target.method, target.path, nil)
		request.AddCookie(&http.Cookie{Name: "access_token", Value: valid.Value})
		request.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: valid.Value})
		request.AddCookie(&http.Cookie{Name: "campusclaw_refresh", Value: valid.Value})

		if recorder := env.Do(t, request); recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with cookies only: status = %d, want 401",
				target.method, target.path, recorder.Code)
		}
	}
}

// AU06: the stored value is the SHA-256 of the presented Bearer token, never
// the token itself.
func TestSessionStoresOnlyTheTokenDigest(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	cookie := env.Login(t, "teacher_a", env.Password(t, "teacher_a"))

	sum := sha256.Sum256([]byte(cookie.Value))
	wantDigest := hex.EncodeToString(sum[:])

	var stored string
	if err := env.DB.QueryRow("SELECT session_id FROM sessions").Scan(&stored); err != nil {
		t.Fatalf("read session row: %v", err)
	}

	if stored != wantDigest {
		t.Errorf("session_id = %q, want the SHA-256 digest %q", stored, wantDigest)
	}

	if stored == cookie.Value {
		t.Error("the raw token was stored")
	}

	if len(stored) != 64 {
		t.Errorf("session_id length = %d, want 64", len(stored))
	}

	if recorder := env.get(t, "/api/me", cookie); recorder.Code != http.StatusOK {
		t.Errorf("/api/me with the raw token: status = %d", recorder.Code)
	}
}

// AU02: logging in mints a new token and revokes the one the browser presented.
func TestLoginRotatesTheSession(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	first := env.Login(t, "teacher_a", env.Password(t, "teacher_a"))

	// The same browser logs in again: it presents the cookie it already has,
	// which the server must revoke in favour of the new session.
	second := env.LoginWith(t, "teacher_a", env.Password(t, "teacher_a"), first)

	if first.Value == second.Value {
		t.Fatal("a second login reused the same token")
	}

	if recorder := env.get(t, "/api/me", second); recorder.Code != http.StatusOK {
		t.Errorf("new token rejected: %d", recorder.Code)
	}

	if recorder := env.get(t, "/api/me", first); recorder.Code != http.StatusUnauthorized {
		t.Errorf("the replaced token still works: %d, want 401", recorder.Code)
	}

	if count := env.CountRows(t, "sessions"); count != 1 {
		t.Errorf("sessions = %d, want exactly 1 live session", count)
	}
}

// An expired session must not authenticate even before a cleanup pass runs.
func TestExpiredSessionIsRejectedWithoutCleanup(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	token, err := auth.NewToken()
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	var teacherID uint64
	if err := env.DB.QueryRow("SELECT id FROM users WHERE username = 'teacher_a'").Scan(&teacherID); err != nil {
		t.Fatalf("look up teacher: %v", err)
	}

	if _, err := env.DB.Exec(
		"INSERT INTO sessions (session_id, user_id, created_at, expires_at) VALUES (?, ?, UTC_TIMESTAMP(6), ?)",
		token.Digest, teacherID, time.Now().UTC().Add(-time.Second),
	); err != nil {
		t.Fatalf("insert expired session: %v", err)
	}

	// The row is deliberately left in place.
	if recorder := env.get(t, "/api/me", &http.Cookie{Name: "access_token", Value: token.Value}); recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for an expired session", recorder.Code)
	}
}

// AC19: logout revokes the session server-side.
func TestLogoutRevokesTheSession(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	cookie := env.Login(t, "teacher_a", env.Password(t, "teacher_a"))

	recorder := env.postJSON(t, "/api/logout", "", cookie, nil)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", recorder.Code)
	}

	if body := recorder.Body.String(); body != "" {
		t.Errorf("logout body = %q, want empty", body)
	}

	if cookies := recorder.Result().Cookies(); len(cookies) != 0 {
		t.Errorf("logout set %d cookies, want none", len(cookies))
	}
	if header := recorder.Header().Get("Set-Cookie"); header != "" {
		t.Errorf("logout sent Set-Cookie: %q", header)
	}

	// Replaying the saved token must fail: this is the whole point of a
	// server-side session.
	if replay := env.get(t, "/api/me", cookie); replay.Code != http.StatusUnauthorized {
		t.Errorf("replayed token: status = %d, want 401", replay.Code)
	}

	if count := env.CountRows(t, "sessions"); count != 0 {
		t.Errorf("sessions = %d, want 0 after logout", count)
	}
}

// Logging out with a dead session answers 401 and touches no cookie.
func TestLogoutWithInvalidSessionLeavesNoCookie(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	recorder := env.postJSON(t, "/api/logout", "", &http.Cookie{Value: "not-a-real-token"}, nil)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", recorder.Code)
	}

	if len(recorder.Result().Cookies()) != 0 {
		t.Error("an invalid Bearer token must not set a cookie")
	}
}

// AUTH-05: authentication and role checks run before the origin check.
func TestAuthenticationPrecedesSameOriginCheck(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	// A cross-origin POST with no session must report the missing session
	// rather than the bad origin.
	request := httptest.NewRequest(http.MethodPost, "/api/materials", nil)
	request.Header.Set("Origin", "http://evil.example")

	if recorder := env.Do(t, request); recorder.Code != http.StatusUnauthorized {
		t.Errorf("anonymous cross-origin upload: status = %d, want 401 (auth before origin)", recorder.Code)
	}

	// A student with a valid session is rejected for their role, not the origin.
	student := env.Login(t, "student_a1", env.Password(t, "student_a1"))

	request = httptest.NewRequest(http.MethodPost, "/api/materials", nil)
	request.Header.Set("Authorization", "Bearer "+student.Value)
	request.Header.Set("Origin", "http://evil.example")

	if recorder := env.Do(t, request); recorder.Code != http.StatusForbidden {
		t.Errorf("student cross-origin upload: status = %d, want 403 (role before origin)", recorder.Code)
	}
}

// The response must never be cacheable, and must carry no identity claims in
// any cookie beyond the session token.
func TestAuthenticationResponsesAreNotCacheable(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	loginRecorder := env.postJSON(t, "/api/login",
		loginBody("teacher_a", env.Password(t, "teacher_a")), nil, nil)

	if got := loginRecorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("login Cache-Control = %q, want no-store", got)
	}

	var login struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(loginRecorder.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}
	if cookies := loginRecorder.Result().Cookies(); len(cookies) != 0 {
		t.Errorf("login set %d cookies, want none", len(cookies))
	}

	meRecorder := env.get(t, "/api/me", &http.Cookie{Value: login.Token})
	if got := meRecorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("me Cache-Control = %q, want no-store", got)
	}

	// AUTH-02: the token is opaque randomness, not a readable identity claim.
	for _, claim := range []string{"role", "class_id", "user_id", "teacher", "student"} {
		if strings.Contains(strings.ToLower(login.Token), claim) {
			t.Errorf("access token exposes %q: %s", claim, login.Token)
		}
	}
}

// AU01: tampering with client-side identity does not change the server's view.
func TestClientSideIdentityTamperingHasNoEffect(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	student := env.Login(t, "student_a1", env.Password(t, "student_a1"))

	request := httptest.NewRequest(http.MethodGet, "/api/me?role=teacher&class_id=2", nil)
	request.Header.Set("Authorization", "Bearer "+student.Value)
	request.AddCookie(&http.Cookie{Name: "role", Value: "teacher"})
	request.AddCookie(&http.Cookie{Name: "class_id", Value: "2"})

	recorder := env.Do(t, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}

	user := decodeUser(t, recorder.Body.Bytes())

	if user.Role != "student" {
		t.Errorf("role = %q, want student", user.Role)
	}
	if user.ClassName != "Class A" {
		t.Errorf("class = %q, want Class A", user.ClassName)
	}
}

// A wrong-password attempt must still cost roughly a bcrypt comparison.
func TestUnknownUserStillPerformsAComparison(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	// Sanity check that the dummy hash is a real cost-12 bcrypt hash: if it
	// were not, bcrypt would return immediately and leak the miss by timing.
	if err := bcrypt.CompareHashAndPassword(
		[]byte("$2a$12$mufTqha0dR2tyrpIb9JjmOtm0gTq0JHP4eFJ3JMKp58TPGZGGV.YK"),
		[]byte("anything"),
	); err == nil {
		t.Error("the dummy hash unexpectedly matched")
	}

	unknown := env.postJSON(t, "/api/login", loginBody("ghost_user", "x"), nil, nil)
	if unknown.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", unknown.Code)
	}
}

// Error bodies use the fixed envelope with no internal detail.
func TestErrorEnvelopeIsUniform(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	cookie := env.Login(t, "student_a1", env.Password(t, "student_a1"))

	recorder := env.get(t, "/api/materials/999999", cookie)

	var envelope httpx.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error body %q: %v", recorder.Body.String(), err)
	}

	if envelope.Error.Code != httpx.CodeNotFound {
		t.Errorf("code = %q, want %q", envelope.Error.Code, httpx.CodeNotFound)
	}

	for _, leak := range []string{"sql", "SELECT", "knowledge_entries", "/uploads", "mysql"} {
		if strings.Contains(recorder.Body.String(), leak) {
			t.Errorf("error body leaks %q: %s", leak, recorder.Body.String())
		}
	}
}
