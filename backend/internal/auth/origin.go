package auth

import (
	"net/http"
	"net/url"
	"strings"

	"campusclaw/internal/httpx"
)

// Header names are canonicalised by net/http, so they can be looked up
// directly. Presence must be tested separately from the value: a header that
// is present but empty is a rejection, not an absence.
const (
	headerOrigin    = "Origin"
	headerReferer   = "Referer"
	headerFetchSite = "Sec-Fetch-Site"
)

// SameOrigin rejects state-changing requests that a browser reported as coming
// from somewhere else. It is applied to POST routes only.
//
// The proxy in front of the API does not enable CORS, so this is a second line
// of defence specifically against cross-site form posts, which browsers send
// with cookies attached.
func SameOrigin(publicOrigin *url.URL, devPublicOrigin ...*url.URL) func(http.Handler) http.Handler {
	allowed := []*url.URL{publicOrigin}
	for _, origin := range devPublicOrigin {
		if origin != nil {
			allowed = append(allowed, origin)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isSameOriginRequest(r, allowed) {
				httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isSameOriginRequest(r *http.Request, allowed []*url.URL) bool {
	// Sec-Fetch-Site can only ever reject. A cross-site value is refused even
	// when Origin happens to match, which is what the spec requires.
	if fetchSite, present := headerValue(r, headerFetchSite); present {
		if fetchSite != "same-origin" && fetchSite != "none" {
			return false
		}
	}

	// Origin takes precedence and never falls back to Referer, so a request
	// carrying a wrong Origin cannot be rescued by a correct one.
	if origin, present := headerValue(r, headerOrigin); present {
		for _, candidate := range allowed {
			if origin == candidate.String() {
				return true
			}
		}
		return false
	}

	if referer, present := headerValue(r, headerReferer); present {
		for _, candidate := range allowed {
			if sameOriginURL(referer, candidate) {
				return true
			}
		}
		return false
	}

	// No browser context headers at all: a non-browser API client. The request
	// still has to pass authentication and role checks downstream.
	return true
}

// headerValue distinguishes "absent" from "present but empty".
func headerValue(r *http.Request, name string) (string, bool) {
	values, ok := r.Header[name]
	if !ok || len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// sameOriginURL compares scheme, host and effective port, so
// "https://host" and "https://host:443" are treated as the same origin.
func sameOriginURL(raw string, want *url.URL) bool {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return false
	}

	if !strings.EqualFold(parsed.Scheme, want.Scheme) {
		return false
	}

	if !strings.EqualFold(parsed.Hostname(), want.Hostname()) {
		return false
	}

	return effectivePort(parsed) == effectivePort(want)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}

	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}
