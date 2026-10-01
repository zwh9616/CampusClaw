package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"regexp"
	"time"
)

const (
	SessionLifetime = 24 * time.Hour
	tokenBytes      = 32
)

var bearerPattern = regexp.MustCompile("(?i:^Bearer) ([A-Za-z0-9_-]{43})$")

type Token struct {
	Value  string
	Digest string
}

func NewToken() (Token, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return Token{}, err
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	return Token{Value: value, Digest: DigestToken(value)}, nil
}

func DigestToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validToken(value string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(raw) == tokenBytes && base64.RawURLEncoding.EncodeToString(raw) == value
}

// BearerToken reads the single Authorization header. Cookies, query parameters
// and request bodies are never consulted, so a browser that still carries an
// old cookie cannot authenticate with it.
func BearerToken(r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}
	match := bearerPattern.FindStringSubmatch(values[0])
	if len(match) != 2 || !validToken(match[1]) {
		return "", false
	}
	return match[1], true
}
