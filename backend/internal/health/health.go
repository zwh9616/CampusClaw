// Package health serves the unauthenticated readiness probe.
package health

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"campusclaw/internal/httpx"
)

// pingTimeout bounds the readiness check so a hung database cannot make the
// probe hang with it.
const pingTimeout = 2 * time.Second

// Handler answers GET /health.
type Handler struct {
	db *sql.DB
}

// New builds the readiness handler.
func New(db *sql.DB) *Handler {
	return &Handler{db: db}
}

// ServeHTTP reports 200 only when the database answers in time.
//
// The failure body is the generic error envelope: it carries no DSN, driver
// detail or credential, which matters because this endpoint is public.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
	defer cancel()

	if err := h.db.PingContext(ctx); err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeServiceUnavailable)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
