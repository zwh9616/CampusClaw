package tests

import (
	"bytes"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"campusclaw/internal/materials"
	"campusclaw/internal/server"
)

// MAT-05: only one PDF or DOCX is parsed at a time, and a request that cannot
// get a slot is told the parser is busy rather than queueing forever.
func TestParserSlotTimeoutIsReportedAsBusy(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	limiter := materials.NewParseLimiter(1, 50*time.Millisecond)

	// Hold the only slot for the duration of the request.
	if err := limiter.Acquire(t.Context()); err != nil {
		t.Fatalf("occupy the parse slot: %v", err)
	}
	defer limiter.Release()

	handler := env.ServerWith(server.WithParseLimiter(limiter))
	teacher := teacherCookie(t, env)

	filesBefore := env.countStoredFiles(t)

	busy := env.uploadVia(t, handler, teacher, uploadRequest{
		title: "Busy", filename: "doc.pdf", content: buildCJKPDF("Busy", "忙"),
	})

	if busy.Code != http.StatusServiceUnavailable {
		t.Errorf("PDF while busy: status = %d, want 503 (body %s)", busy.Code, busy.Body.String())
	}

	if got := env.CountRows(t, "materials"); got != 0 {
		t.Errorf("materials = %d, want 0: a busy parser must not create rows", got)
	}
	if got := env.countStoredFiles(t); got != filesBefore {
		t.Errorf("stored files = %d, want %d: the saved original must be removed", got, filesBefore)
	}

	// The text formats do not queue for a parser slot, so they still succeed.
	text := env.uploadVia(t, handler, teacher, uploadRequest{
		title: "Text", filename: "notes.md", content: []byte("no slot needed"),
	})

	if text.Code != http.StatusCreated {
		t.Errorf("markdown while the parser is busy: status = %d, want 201", text.Code)
	}
}

// RUN-04 / AC21: /health needs no session and reflects the database.
func TestHealthReflectsDatabaseReadiness(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	ready := env.get(t, "/health", nil)
	if ready.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", ready.Code)
	}

	if body := strings.TrimSpace(ready.Body.String()); body != `{"status":"ok"}` {
		t.Errorf("body = %s, want {\"status\":\"ok\"}", body)
	}

	// Take the database away: the probe must notice.
	if err := env.DB.Close(); err != nil {
		t.Fatalf("close pool: %v", err)
	}

	unavailable := env.get(t, "/health", nil)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Errorf("status with the database down = %d, want 503", unavailable.Code)
	}

	// The public failure body must not describe the connection.
	for _, leak := range []string{"tcp(", env.Config.MySQL.Password, env.Config.MySQL.Name} {
		if leak != "" && strings.Contains(unavailable.Body.String(), leak) {
			t.Errorf("health body leaks %q: %s", leak, unavailable.Body.String())
		}
	}
}

// RU02: the runtime image must actually carry the document parsers, since the
// API refuses to start without them.
func TestDocumentParsersAreInstalled(t *testing.T) {
	if err := materials.VerifyParserTools(); err != nil {
		t.Errorf("VerifyParserTools() = %v, want nil in an image that can parse PDFs", err)
	}
}

// AUTH-05: logs must never contain a password, a session token, a DSN or the
// body of an uploaded file.
func TestLogsNeverContainSecrets(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	var captured bytes.Buffer

	previous := log.Writer()
	log.SetOutput(&captured)
	t.Cleanup(func() { log.SetOutput(previous) })

	teacher := teacherCookie(t, env)
	uploadMarkdown(t, env, teacher, "Log check", "SECRET-MATERIAL-BODY")

	// Exercise a rejection path as well, so the failure logs are covered.
	env.upload(t, teacher, uploadRequest{
		title: "Rejected", filename: "payload.exe", content: []byte("SECRET-MATERIAL-BODY"),
	})

	logged := captured.String()

	secrets := map[string]string{
		"Teacher A password":  env.Password(t, "teacher_a"),
		"Student A1 password": env.Password(t, "student_a1"),
		"Teacher B password":  env.Password(t, "teacher_b"),
		"Student B1 password": env.Password(t, "student_b1"),
		"database password":   env.Config.MySQL.Password,
		"session token":       teacher.Value,
	}

	for name, secret := range secrets {
		if secret == "" {
			continue
		}
		if strings.Contains(logged, secret) {
			t.Errorf("log output contains the %s", name)
		}
	}

	if strings.Contains(logged, "SECRET-MATERIAL-BODY") {
		t.Error("log output contains uploaded file content")
	}

	if strings.Contains(logged, "@tcp(") {
		t.Errorf("log output contains a connection string: %s", logged)
	}
}
