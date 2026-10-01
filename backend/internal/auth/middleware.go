package auth

import (
	"errors"
	"log"
	"net/http"
	"time"

	"campusclaw/internal/httpx"
)

// Authenticator resolves Bearer access tokens against the session store.
type Authenticator struct {
	store *Store
}

// NewAuthenticator builds an authenticator over a session store.
func NewAuthenticator(store *Store) *Authenticator {
	return &Authenticator{store: store}
}

// RequireUser rejects requests without a live session and attaches the
// resolved identity to the request context for inner handlers.
func (a *Authenticator) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := BearerToken(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
			return
		}

		user, err := a.store.Resolve(r.Context(), token, time.Now())
		switch {
		case errors.Is(err, ErrNoSession):
			w.Header().Set("WWW-Authenticate", "Bearer")
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
