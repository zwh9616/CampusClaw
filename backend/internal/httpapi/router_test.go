package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"campusclaw/internal/httpx"
)

func testRouter() http.Handler {
	return NewRouter([]Route{
		{
			Method:  http.MethodGet,
			Pattern: "/api/me",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				httpx.WriteJSON(w, http.StatusOK, map[string]string{"ok": "yes"})
			}),
		},
		{
			Method:  http.MethodGet,
			Pattern: "/api/materials/{id}",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				httpx.WriteJSON(w, http.StatusOK, map[string]string{"ok": "detail"})
			}),
		},
	})
}

func decodeError(t *testing.T, body []byte) httpx.ErrorResponse {
	t.Helper()

	var envelope httpx.ErrorResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return envelope
}

func TestRouterDispatchesRegisteredRoute(t *testing.T) {
	recorder := httptest.NewRecorder()
	testRouter().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/me", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

func TestRouterServesPathParameters(t *testing.T) {
	recorder := httptest.NewRecorder()
	testRouter().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/materials/7", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

func TestRouterAnswersUnknownPathWithJSONNotFound(t *testing.T) {
	for _, path := range []string{"/api/unknown", "/", "/api"} {
		recorder := httptest.NewRecorder()
		testRouter().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, recorder.Code)
			continue
		}

		if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Errorf("%s: Content-Type = %q, want JSON (must never fall back to SPA HTML)", path, got)
		}

		if envelope := decodeError(t, recorder.Body.Bytes()); envelope.Error.Code != httpx.CodeNotFound {
			t.Errorf("%s: code = %q, want %q", path, envelope.Error.Code, httpx.CodeNotFound)
		}
	}
}

func TestRouterAnswersWrongMethodWithJSON405(t *testing.T) {
	recorder := httptest.NewRecorder()
	testRouter().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/me", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}

	if got := recorder.Header().Get("Allow"); got != http.MethodGet {
		t.Errorf("Allow = %q, want %q", got, http.MethodGet)
	}

	if envelope := decodeError(t, recorder.Body.Bytes()); envelope.Error.Code != httpx.CodeMethodNotAllowed {
		t.Errorf("code = %q, want %q", envelope.Error.Code, httpx.CodeMethodNotAllowed)
	}
}
