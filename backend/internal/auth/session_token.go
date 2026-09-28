package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

const (
	// CookieName is the only cookie the application sets.
	CookieName = "campusclaw_session"

	// SessionLifetime is fixed at 24 hours; the cookie Max-Age matches.
	SessionLifetime = 24 * time.Hour

	// tokenBytes gives 256 bits of randomness, the floor the spec requires.
	tokenBytes = 32
)

// Token pairs the value handed to the browser with the digest stored in the
// database. Only the digest ever reaches the sessions table, so a database
// dump cannot be replayed as a live session.
type Token struct {
	Value  string
	Digest string
}

// NewToken mints a fresh session token.
func NewToken() (Token, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return Token{}, fmt.Errorf("generate session token: %w", err)
	}

	value := base64.RawURLEncoding.EncodeToString(raw)
	return Token{Value: value, Digest: DigestToken(value)}, nil
}

// DigestToken hashes the token's raw ASCII bytes into 64 lowercase hex
// characters, the exact form stored and compared in sessions.session_id.
func DigestToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// SetSessionCookie hands the token to the browser. There is no Domain
// attribute so the cookie stays host-scoped, and Secure is driven by
// configuration because local development runs over plain HTTP.
func SetSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(SessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie expires the cookie using the same Path, Secure and
// SameSite attributes it was set with, so the browser actually replaces it.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0).UTC(),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// CookieToken reads the raw session token from the request, if any.
func CookieToken(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return cookie.Value, true
}
