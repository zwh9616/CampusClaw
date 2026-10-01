package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
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

func TestBearerTokenParsing(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"", "Basic " + token.Value, "Bearer bad", "Bearer " + token.Value + " trailing", "Bearer  " + token.Value} {
		request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		if header != "" {
			request.Header.Set("Authorization", header)
		}
		if _, ok := BearerToken(request); ok {
			t.Errorf("accepted %q", header)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	request.Header.Set("Authorization", "Bearer "+token.Value)
	if got, ok := BearerToken(request); !ok || got != token.Value {
		t.Errorf("BearerToken = %q, %t", got, ok)
	}
	request.Header.Add("Authorization", "Bearer "+token.Value)
	if _, ok := BearerToken(request); ok {
		t.Error("accepted duplicate Authorization headers")
	}
}

// AU07: a cookie never stands in for the Authorization header, whatever its
// name, so a browser still carrying an old session cookie cannot authenticate.
func TestBearerTokenIgnoresCookies(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"campusclaw_session", "campusclaw_refresh", "access_token"} {
		request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		request.AddCookie(&http.Cookie{Name: name, Value: token.Value})
		if got, ok := BearerToken(request); ok {
			t.Errorf("BearerToken accepted cookie %q as %q", name, got)
		}
	}
}
