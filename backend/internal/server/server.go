// Package server assembles the API's dependencies into a single http.Handler.
package server

import (
	"database/sql"
	"net/http"
	"time"

	"campusclaw/internal/auth"
	"campusclaw/internal/config"
	"campusclaw/internal/health"
	"campusclaw/internal/httpapi"
	"campusclaw/internal/httpx"
	"campusclaw/internal/materials"
)

// Only one PDF or DOCX is parsed at a time, and a request waits at most this
// long for the slot before being told the parser is busy.
const (
	parseConcurrency = 1
	parseWait        = 2 * time.Second
)

// options holds the pieces that can be substituted, which the integration
// suite uses to saturate the parse limiter.
type options struct {
	parseLimiter *materials.ParseLimiter
}

// Option customises the assembled server.
type Option func(*options)

// WithParseLimiter replaces the document parse limiter.
func WithParseLimiter(limiter *materials.ParseLimiter) Option {
	return func(o *options) {
		o.parseLimiter = limiter
	}
}

// New builds the router for the given configuration and database pool.
func New(cfg *config.Config, handle *sql.DB, opts ...Option) http.Handler {
	settings := options{
		parseLimiter: materials.NewParseLimiter(parseConcurrency, parseWait),
	}
	for _, opt := range opts {
		opt(&settings)
	}

	sessions := auth.NewStore(handle)
	accounts := auth.NewAccounts(handle)
	authenticator := auth.NewAuthenticator(sessions, cfg.SessionCookieSecure)
	authHandlers := auth.NewHandlers(accounts, sessions, authenticator)

	materialHandlers := materials.NewHandlers(
		materials.NewRepo(handle),
		materials.NewStorage(cfg.UploadDir),
		map[string]materials.Extractor{
			".md":   materials.TextExtractor{},
			".txt":  materials.TextExtractor{},
			".pdf":  materials.NewPDFExtractor(),
			".docx": materials.DOCXExtractor{},
		},
		settings.parseLimiter,
	)

	// SameOrigin is layered *inside* the authentication middleware. Middleware
	// wraps outwards, so the innermost wrapper runs last: this ordering is what
	// guarantees "no session -> 401" is answered before "cross-origin -> 403".
	sameOrigin := auth.SameOrigin(cfg.PublicOrigin, cfg.DevPublicOrigin)

	routes := []httpapi.Route{
		{
			// Unauthenticated by design: this is the container health probe.
			Method:  http.MethodGet,
			Pattern: "/health",
			Handler: health.New(handle),
		},
		{
			Method:  http.MethodPost,
			Pattern: "/api/login",
			Handler: httpx.NoStore(sameOrigin(http.HandlerFunc(authHandlers.Login))),
		},
		{
			Method:  http.MethodPost,
			Pattern: "/api/logout",
			Handler: httpx.NoStore(authenticator.RequireUser(sameOrigin(http.HandlerFunc(authHandlers.Logout)))),
		},
		{
			Method:  http.MethodGet,
			Pattern: "/api/me",
			Handler: httpx.NoStore(authenticator.RequireUser(http.HandlerFunc(authHandlers.Me))),
		},
		{
			Method:  http.MethodGet,
			Pattern: "/api/materials",
			Handler: httpx.NoStore(authenticator.RequireUser(http.HandlerFunc(materialHandlers.List))),
		},
		{
			Method:  http.MethodPost,
			Pattern: "/api/materials",
			// Outermost first: authentication (401), then the teacher check
			// (403), then the origin check (403), and only then a handler that
			// reads the request body.
			Handler: httpx.NoStore(authenticator.RequireUser(
				auth.RequireTeacher(
					sameOrigin(http.HandlerFunc(materialHandlers.Upload)),
				),
			)),
		},
		{
			Method:  http.MethodGet,
			Pattern: "/api/materials/{id}",
			Handler: httpx.NoStore(authenticator.RequireUser(http.HandlerFunc(materialHandlers.Detail))),
		},
		{
			Method:  http.MethodGet,
			Pattern: "/api/materials/{id}/file",
			Handler: httpx.NoStore(authenticator.RequireUser(http.HandlerFunc(materialHandlers.Download))),
		},
	}

	return httpapi.NewRouter(routes)
}
