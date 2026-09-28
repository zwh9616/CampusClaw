// Package httpapi wires routes onto net/http and guarantees that every
// response leaving the API — including 404 and 405 — uses the JSON envelope.
package httpapi

import (
	"net/http"
	"sort"
	"strings"

	"campusclaw/internal/httpx"
)

// Route binds one HTTP method to a Go 1.22 style pattern such as
// "/api/materials/{id}".
type Route struct {
	Method  string
	Pattern string
	Handler http.Handler
}

// NewRouter registers routes on a ServeMux and adds fallbacks so unknown paths
// and unsupported methods answer with JSON instead of the mux's plain text.
func NewRouter(routes []Route) http.Handler {
	mux := http.NewServeMux()
	allowed := make(map[string][]string)

	for _, route := range routes {
		mux.Handle(route.Method+" "+route.Pattern, route.Handler)
		allowed[route.Pattern] = append(allowed[route.Pattern], route.Method)
	}

	for pattern, methods := range allowed {
		mux.Handle(pattern, methodNotAllowed(methods))
	}

	// Least specific pattern: catches every path no route claimed, so unknown
	// API paths return JSON rather than being rewritten to the SPA.
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound)
	}))

	return mux
}

func methodNotAllowed(methods []string) http.Handler {
	sorted := append([]string(nil), methods...)
	sort.Strings(sorted)
	allow := strings.Join(sorted, ", ")

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", allow)
		httpx.WriteError(w, http.StatusMethodNotAllowed, httpx.CodeMethodNotAllowed)
	})
}
