package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// guardedHandler returns the SameOrigin-guarded handler plus a recorder, and
// sends one POST carrying exactly the headers under test.
func runOriginGuard(t *testing.T, publicOrigin string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	parsed, err := url.Parse(publicOrigin)
	if err != nil {
		t.Fatalf("parse %q: %v", publicOrigin, err)
	}

	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusCreated)
	})

	request := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	for name, value := range headers {
		// Assigning through the map keeps a header that is present but empty,
		// which Set would collapse into an absent header.
		request.Header[name] = []string{value}
	}

	recorder := httptest.NewRecorder()
	SameOrigin(parsed)(next).ServeHTTP(recorder, request)

	if recorder.Code == http.StatusCreated && !reached {
		t.Error("handler reported success without being reached")
	}

	return recorder
}

func TestSameOriginAllowsRequestWithNoContextHeaders(t *testing.T) {
	// A non-browser client sends none of the three headers; it is allowed
	// through to the authentication checks that follow.
	recorder := runOriginGuard(t, "http://localhost:8080", nil)

	if recorder.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", recorder.Code)
	}
}

func TestSameOriginAcceptsMatchingOrigin(t *testing.T) {
	recorder := runOriginGuard(t, "http://localhost:8080", map[string]string{
		headerOrigin: "http://localhost:8080",
	})

	if recorder.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", recorder.Code)
	}
}

// AU03 and AU05: a foreign origin is always refused.
func TestSameOriginRejectsForeignOrigin(t *testing.T) {
	cases := map[string]string{
		"other host":   "http://evil.example",
		"other port":   "http://localhost:9999",
		"other scheme": "https://localhost:8080",
		"null origin":  "null",
		"empty origin": "",
		"garbage":      "not a url",
	}

	for name, origin := range cases {
		recorder := runOriginGuard(t, "http://localhost:8080", map[string]string{headerOrigin: origin})

		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s (%q): status = %d, want 403", name, origin, recorder.Code)
		}
	}
}

func TestSameOriginAcceptsMatchingRefererWhenOriginAbsent(t *testing.T) {
	for name, referer := range map[string]string{
		"same origin":        "http://localhost:8080/",
		"same origin page":   "http://localhost:8080/materials?x=1",
		"explicit same port": "http://localhost:8080/index.html",
	} {
		recorder := runOriginGuard(t, "http://localhost:8080", map[string]string{headerReferer: referer})

		if recorder.Code != http.StatusCreated {
			t.Errorf("%s (%q): status = %d, want 201", name, referer, recorder.Code)
		}
	}
}

func TestSameOriginRejectsForeignOrMalformedReferer(t *testing.T) {
	cases := map[string]string{
		"other host":    "http://evil.example/page",
		"other port":    "http://localhost:9999/page",
		"other scheme":  "https://localhost:8080/page",
		"empty referer": "",
		"not absolute":  "localhost:8080/page",
		"garbage":       "////",
	}

	for name, referer := range cases {
		recorder := runOriginGuard(t, "http://localhost:8080", map[string]string{headerReferer: referer})

		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s (%q): status = %d, want 403", name, referer, recorder.Code)
		}
	}
}

// AU05: Origin wins and never falls back to Referer.
func TestSameOriginDoesNotFallBackToReferer(t *testing.T) {
	recorder := runOriginGuard(t, "http://localhost:8080", map[string]string{
		headerOrigin:  "http://evil.example",
		headerReferer: "http://localhost:8080/materials",
	})

	if recorder.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403: a bad Origin must not be rescued by a good Referer", recorder.Code)
	}
}

// AU04: a matching Origin is enough; Referer is not required alongside it.
func TestSameOriginDoesNotRequireReferer(t *testing.T) {
	recorder := runOriginGuard(t, "http://localhost:8080", map[string]string{
		headerOrigin: "http://localhost:8080",
	})

	if recorder.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", recorder.Code)
	}
}

func TestSameOriginSecFetchSiteHandling(t *testing.T) {
	cases := map[string]struct {
		headers map[string]string
		want    int
	}{
		"fetch-site same-origin alone": {
			headers: map[string]string{headerFetchSite: "same-origin"},
			want:    http.StatusCreated,
		},
		"fetch-site none alone": {
			headers: map[string]string{headerFetchSite: "none"},
			want:    http.StatusCreated,
		},
		"fetch-site same-site alone": {
			headers: map[string]string{headerFetchSite: "same-site"},
			want:    http.StatusForbidden,
		},
		"fetch-site cross-site alone": {
			headers: map[string]string{headerFetchSite: "cross-site"},
			want:    http.StatusForbidden,
		},
		"matching origin with cross-site": {
			headers: map[string]string{
				headerOrigin:    "http://localhost:8080",
				headerFetchSite: "cross-site",
			},
			want: http.StatusForbidden,
		},
		"matching origin with same-origin": {
			headers: map[string]string{
				headerOrigin:    "http://localhost:8080",
				headerFetchSite: "same-origin",
			},
			want: http.StatusCreated,
		},
		"matching referer with cross-site": {
			headers: map[string]string{
				headerReferer:   "http://localhost:8080/",
				headerFetchSite: "cross-site",
			},
			want: http.StatusForbidden,
		},
	}

	for name, testCase := range cases {
		recorder := runOriginGuard(t, "http://localhost:8080", testCase.headers)

		if recorder.Code != testCase.want {
			t.Errorf("%s: status = %d, want %d", name, recorder.Code, testCase.want)
		}
	}
}

func TestSameOriginTreatsDefaultPortsAsEquivalent(t *testing.T) {
	// The Origin header is matched literally, but a Referer is compared by
	// scheme, host and effective port, so the explicit default port matches.
	recorder := runOriginGuard(t, "https://campus.example.edu", map[string]string{
		headerReferer: "https://campus.example.edu:443/materials",
	})

	if recorder.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", recorder.Code)
	}

	implicit := runOriginGuard(t, "https://campus.example.edu", map[string]string{
		headerReferer: "https://campus.example.edu/materials",
	})

	if implicit.Code != http.StatusCreated {
		t.Errorf("implicit default port: status = %d, want 201", implicit.Code)
	}
}

func TestRequireTeacherGatesByRole(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	cases := map[string]struct {
		user User
		have bool
		want int
	}{
		"no identity": {user: User{}, have: false, want: http.StatusUnauthorized},
		"teacher":     {user: User{Role: RoleTeacher}, have: true, want: http.StatusCreated},
		"student":     {user: User{Role: RoleStudent}, have: true, want: http.StatusForbidden},
		"unknown role": {user: User{Role: "admin"}, have: true, want: http.StatusForbidden},
	}

	for name, testCase := range cases {
		request := httptest.NewRequest(http.MethodPost, "/api/materials", nil)
		ctx := request.Context()
		if testCase.have {
			ctx = WithUser(ctx, testCase.user)
		}
		request = request.WithContext(ctx)

		recorder := httptest.NewRecorder()
		RequireTeacher(next).ServeHTTP(recorder, request)

		if recorder.Code != testCase.want {
			t.Errorf("%s: status = %d, want %d", name, recorder.Code, testCase.want)
		}
	}
}
