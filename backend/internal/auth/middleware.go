package auth

import (
	"errors"
	"log"
	"net/http"
	"time"

	"campusclaw/internal/httpx"
)

// Authenticator resolves session cookies and owns the cookie attributes used
// when a session is handed out or cleared.
type Authenticator struct {
	store  *Store
	secure bool
}

// NewAuthenticator builds an authenticator over a session store.
func NewAuthenticator(store *Store, secure bool) *Authenticator {
	return &Authenticator{store: store, secure: secure}
}

// Secure reports whether cookies carry the Secure attribute in this
// deployment, so handlers set and clear them with matching attributes.
func (a *Authenticator) Secure() bool {
	return a.secure
}

// RequireUser rejects requests without a live session and attaches the
// resolved identity to the request context for inner handlers.
func (a *Authenticator) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := CookieToken(r)
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
			return
		}

		user, err := a.store.Resolve(r.Context(), token, time.Now())
		switch {
		case errors.Is(err, ErrNoSession):
			// The cookie cannot authenticate anyone, so expire it. This is
			// also what makes POST /api/logout answer 401 *and* clear the
			// cookie for a dead session, while still running before the
			// same-origin check.
			ClearSessionCookie(w, a.secure)
			httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
			return
		case err != nil:
			log.Printf("auth: resolve session: %v", err)
			httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal)
			return
		}

		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}

// RequireTeacher must be layered inside RequireUser. It answers 403 for any
// authenticated non-teacher.
//
// Because it runs as middleware it completes before the wrapped handler reads
// a single byte of the request body, which is what makes "students uploading
// never write a file or a row" hold even for malformed multipart.
func RequireTeacher(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFrom(r.Context())
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
			return
		}

		if !user.IsTeacher() {
			httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
