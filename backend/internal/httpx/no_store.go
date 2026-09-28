package httpx

import "net/http"

// NoStore marks a response as uncacheable. It wraps authentication responses
// and all protected data so a shared or browser cache can never replay another
// user's identity or material.
func NoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
