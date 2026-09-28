package auth

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"campusclaw/internal/config"
	"campusclaw/internal/httpx"
)

// dummyPasswordHash is a valid cost-12 bcrypt hash of a value no account has.
// Unknown usernames are compared against it so that a missing account costs
// the same wall-clock time as a wrong password.
const dummyPasswordHash = "$2a$12$mufTqha0dR2tyrpIb9JjmOtm0gTq0JHP4eFJ3JMKp58TPGZGGV.YK"

// maxLoginBody bounds how much JSON the login endpoint buffers.
const maxLoginBody = 4 << 10

// Handlers serves the authentication endpoints.
type Handlers struct {
	accounts *Accounts
	sessions *Store
	auth     *Authenticator
}

// NewHandlers wires the authentication endpoints together.
func NewHandlers(accounts *Accounts, sessions *Store, authenticator *Authenticator) *Handlers {
	return &Handlers{accounts: accounts, sessions: sessions, auth: authenticator}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	User User `json:"user"`
}

// Login authenticates a username and password and issues a fresh session.
//
// Unknown fields in the body are ignored on purpose: the spec requires that a
// client-supplied role or class_id has no effect rather than being rejected.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, maxLoginBody)
	defer body.Close()

	decoder := json.NewDecoder(body)

	var request loginRequest
	if err := decoder.Decode(&request); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest)
		return
	}

	// Exactly one JSON value is expected; trailing content means a malformed body.
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest)
		return
	}

	if request.Username == "" || request.Password == "" {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest)
		return
	}

	// Passwords are raw bytes: no trimming, no truncation. A value beyond
	// bcrypt's window is never handed to the hasher and simply fails login.
	password := []byte(request.Password)
	if len(password) > config.MaxPasswordBytes {
		log.Printf("auth: login rejected reason=password_too_long")
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
		return
	}

	account, err := h.accounts.ByUsername(r.Context(), request.Username)
	switch {
	case errors.Is(err, ErrNoAccount):
		// Burn an equivalent comparison so response time does not disclose
		// whether the username exists.
		_ = bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), password)
		log.Printf("auth: login rejected reason=unknown_account")
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
		return
	case err != nil:
		log.Printf("auth: account lookup failed: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), password); err != nil {
		log.Printf("auth: login rejected reason=bad_password")
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
		return
	}

	// Logging in replaces whatever session the browser arrived with, so a
	// stolen or stale cookie stops working the moment the owner logs in again.
	previousToken, _ := CookieToken(r)

	token, err := h.sessions.Rotate(r.Context(), uint64(account.User.ID), previousToken, time.Now())
	if err != nil {
		log.Printf("auth: create session failed: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal)
		return
	}

	SetSessionCookie(w, token, h.auth.Secure())

	// The token is deliberately absent from the body.
	httpx.WriteJSON(w, http.StatusOK, loginResponse{User: account.User})
}

// Me returns the identity resolved from the session cookie.
func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, user)
}

// Logout deletes the server-side session and expires the cookie.
//
// It sits behind RequireUser, so an invalid or absent session has already been
// answered with 401 and a cleared cookie by the time this runs.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	token, ok := CookieToken(r)
	if !ok {
		ClearSessionCookie(w, h.auth.Secure())
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
		return
	}

	if err := h.sessions.Delete(r.Context(), token); err != nil {
		log.Printf("auth: delete session failed: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal)
		return
	}

	ClearSessionCookie(w, h.auth.Secure())

	w.WriteHeader(http.StatusNoContent)
}
