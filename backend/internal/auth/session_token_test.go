package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

var lowercaseHex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestNewTokenIsUniqueAndUnpredictable(t *testing.T) {
	const iterations = 512

	seen := make(map[string]bool, iterations)

	for i := 0; i < iterations; i++ {
		token, err := NewToken()
		if err != nil {
			t.Fatalf("NewToken() error = %v", err)
		}

		if seen[token.Value] {
			t.Fatalf("NewToken() repeated a value after %d draws", i)
		}
		seen[token.Value] = true
	}
}

func TestNewTokenCarries256Bits(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken() error = %v", err)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(token.Value)
	if err != nil {
		t.Fatalf("token %q is not base64url: %v", token.Value, err)
	}

	if len(decoded) != tokenBytes {
		t.Errorf("token carries %d bytes of randomness, want %d", len(decoded), tokenBytes)
	}
}

// AU06: the stored value must be exactly the SHA-256 of the token's ASCII
// bytes, and must differ from the token itself.
func TestDigestTokenIsSha256OfAsciiBytes(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken() error = %v", err)
	}

	sum := sha256.Sum256([]byte(token.Value))
	want := hex.EncodeToString(sum[:])

	if token.Digest != want {
		t.Errorf("Digest = %q, want %q", token.Digest, want)
	}

	if !lowercaseHex64.MatchString(token.Digest) {
		t.Errorf("Digest %q is not 64 lowercase hex characters", token.Digest)
	}

	if token.Digest == token.Value {
		t.Error("stored digest equals the raw token")
	}
}

func TestDigestTokenMatchesKnownVector(t *testing.T) {
	// sha256("abc"), the canonical test vector, guards against an accidental
	// change of encoding or alphabet.
	const token = "abc"
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"

	if got := DigestToken(token); got != want {
		t.Errorf("DigestToken(%q) = %q, want %q", token, got, want)
	}
}

func TestSetSessionCookieAttributes(t *testing.T) {
	recorder := httptest.NewRecorder()
	SetSessionCookie(recorder, "token-value", false)

	cookie := sessionCookie(t, recorder)

	if cookie.Name != CookieName {
		t.Errorf("Name = %q, want %q", cookie.Name, CookieName)
	}
	if !cookie.HttpOnly {
		t.Error("HttpOnly is not set")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("Path = %q, want /", cookie.Path)
	}
	if cookie.MaxAge != int(SessionLifetime.Seconds()) {
		t.Errorf("Max-Age = %d, want %d", cookie.MaxAge, int(SessionLifetime.Seconds()))
	}
	if cookie.Secure {
		t.Error("Secure is set for a plain-HTTP deployment")
	}
	if cookie.Domain != "" {
		t.Errorf("Domain = %q, want empty so the cookie stays host-scoped", cookie.Domain)
	}
}

func TestSetSessionCookieHonoursSecureFlag(t *testing.T) {
	recorder := httptest.NewRecorder()
	SetSessionCookie(recorder, "token-value", true)

	if cookie := sessionCookie(t, recorder); !cookie.Secure {
		t.Error("Secure is not set for an HTTPS deployment")
	}
}

// AU01: the cookie must carry the token and nothing else — no identity claims.
func TestSessionCookieCarriesNoIdentity(t *testing.T) {
	recorder := httptest.NewRecorder()
	SetSessionCookie(recorder, "token-value", true)

	cookie := sessionCookie(t, recorder)

	if cookie.Value != "token-value" {
		t.Errorf("Value = %q, want the raw token", cookie.Value)
	}

	for _, claim := range []string{"role", "class_id", "user_id", "teacher", "student"} {
		if strings.Contains(strings.ToLower(cookie.String()), claim) {
			t.Errorf("cookie contains identity claim %q: %s", claim, cookie.String())
		}
	}
}

func TestClearSessionCookieExpiresWithMatchingAttributes(t *testing.T) {
	set := httptest.NewRecorder()
	SetSessionCookie(set, "token-value", true)
	original := sessionCookie(t, set)

	clearedRecorder := httptest.NewRecorder()
	ClearSessionCookie(clearedRecorder, true)
	cleared := sessionCookie(t, clearedRecorder)

	if cleared.MaxAge != -1 {
		t.Errorf("Max-Age = %d, want -1", cleared.MaxAge)
	}

	if !cleared.Expires.Before(time.Now()) {
		t.Errorf("Expires = %v, want a past time", cleared.Expires)
	}

	if cleared.Value != "" {
		t.Errorf("Value = %q, want empty", cleared.Value)
	}

	// Attributes must match the original or the browser keeps the old cookie.
	if cleared.Path != original.Path || cleared.HttpOnly != original.HttpOnly ||
		cleared.SameSite != original.SameSite || cleared.Secure != original.Secure {
		t.Errorf("cleared cookie attributes differ from the set cookie:\nset:     %s\ncleared: %s",
			original.String(), cleared.String())
	}
}

func TestCookieTokenRejectsAbsentAndEmpty(t *testing.T) {
	absent := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	if _, ok := CookieToken(absent); ok {
		t.Error("CookieToken reported a token for a request with no cookie")
	}

	empty := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	empty.AddCookie(&http.Cookie{Name: CookieName, Value: ""})
	if _, ok := CookieToken(empty); ok {
		t.Error("CookieToken reported a token for an empty cookie")
	}

	present := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	present.AddCookie(&http.Cookie{Name: CookieName, Value: "abc"})
	if got, ok := CookieToken(present); !ok || got != "abc" {
		t.Errorf("CookieToken() = %q, %v; want abc, true", got, ok)
	}
}

func sessionCookie(t *testing.T, recorder *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want exactly 1", len(cookies))
	}
	return cookies[0]
}
