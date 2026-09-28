package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteErrorUsesFixedEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()

	WriteError(recorder, http.StatusNotFound, CodeNotFound)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}

	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}

	var body ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}

	if body.Error.Code != CodeNotFound {
		t.Errorf("code = %q, want %q", body.Error.Code, CodeNotFound)
	}

	if body.Error.Message == "" {
		t.Error("message is empty")
	}
}

func TestWriteErrorFallsBackToInternalForUnknownCode(t *testing.T) {
	recorder := httptest.NewRecorder()

	WriteError(recorder, http.StatusInternalServerError, "made_up_code")

	var body ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
	}

	if body.Error.Code != CodeInternal {
		t.Errorf("code = %q, want %q", body.Error.Code, CodeInternal)
	}
}

func TestWriteErrorNeverLeaksCallerText(t *testing.T) {
	recorder := httptest.NewRecorder()

	// Codes are the only caller-controlled part of an error response.
	WriteError(recorder, http.StatusInternalServerError, "SELECT * FROM users WHERE password='x'")

	if strings.Contains(recorder.Body.String(), "SELECT") {
		t.Errorf("response echoed caller text: %s", recorder.Body.String())
	}
}

func TestWriteJSONEncodesValue(t *testing.T) {
	recorder := httptest.NewRecorder()

	WriteJSON(recorder, http.StatusOK, map[string]string{"status": "ok"})

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	if got := recorder.Body.String(); got != `{"status":"ok"}` {
		t.Errorf("body = %s", got)
	}
}
