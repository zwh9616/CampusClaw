// Command acceptance exercises the running stack through its only public
// entry point, Nginx on port 8080, and prints one PASS/FAIL line per
// acceptance criterion.
//
// It never talks to the API container or the database directly: everything goes
// through the same origin a browser uses, with real cookies, so the run covers
// authentication, tenancy, parsing and the gateway together.
//
// Raw database row counts are asserted by the Go integration suite in
// backend/tests, which runs against the same database from inside the network.
// This runner verifies the same facts through their observable effects.
//
// Configuration (all optional, with sensible defaults):
//
//	ACCEPTANCE_BASE_URL                 default http://localhost:8080
//	ACCEPTANCE_TEACHER_A_PASSWORD       required
//	ACCEPTANCE_STUDENT_A1_PASSWORD      required
//	ACCEPTANCE_STUDENT_B1_PASSWORD      required
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	defaultBaseURL = "http://localhost:8080"
	maxBodyBytes   = 8 << 20
	requestTimeout = 30 * time.Second
)

func main() {
	baseURL := valueOr(os.Getenv("ACCEPTANCE_BASE_URL"), defaultBaseURL)

	passwords := map[string]string{
		"teacher_a":  os.Getenv("ACCEPTANCE_TEACHER_A_PASSWORD"),
		"student_a1": os.Getenv("ACCEPTANCE_STUDENT_A1_PASSWORD"),
		"student_b1": os.Getenv("ACCEPTANCE_STUDENT_B1_PASSWORD"),
	}

	for username, password := range passwords {
		if password == "" {
			fmt.Fprintf(os.Stderr, "missing ACCEPTANCE_%s_PASSWORD\n", strings.ToUpper(username))
			os.Exit(2)
		}
	}

	run := &runner{baseURL: baseURL, passwords: passwords}

	if err := run.execute(); err != nil {
		fmt.Fprintf(os.Stderr, "acceptance run could not start: %v\n", err)
		os.Exit(2)
	}

	os.Exit(run.report())
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// ---------------------------------------------------------------- harness ---

// browser is one independent cookie jar, standing in for one browser profile.
type browser struct {
	name   string
	client *http.Client
	base   string
}

func newBrowser(name, base string) (*browser, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	return &browser{
		name:   name,
		client: &http.Client{Jar: jar, Timeout: requestTimeout},
		base:   strings.TrimSuffix(base, "/"),
	}, nil
}

func (b *browser) do(request *http.Request) (*http.Response, []byte, error) {
	response, err := b.client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		return response, nil, err
	}

	return response, body, nil
}

func (b *browser) get(path string) (*http.Response, []byte, error) {
	request, err := http.NewRequest(http.MethodGet, b.base+path, nil)
	if err != nil {
		return nil, nil, err
	}
	return b.do(request)
}

func (b *browser) postJSON(path, body string) (*http.Response, []byte, error) {
	request, err := http.NewRequest(http.MethodPost, b.base+path, strings.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	return b.do(request)
}

func (b *browser) post(path string) (*http.Response, []byte, error) {
	request, err := http.NewRequest(http.MethodPost, b.base+path, nil)
	if err != nil {
		return nil, nil, err
	}
	return b.do(request)
}

type uploadSpec struct {
	title       string
	filename    string
	content     []byte
	contentType string
	fields      map[string]string
	headers     map[string]string
}

func (b *browser) upload(spec uploadSpec) (*http.Response, []byte, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if spec.title != "" {
		_ = writer.WriteField("title", spec.title)
	}
	for name, value := range spec.fields {
		_ = writer.WriteField(name, value)
	}

	if spec.filename != "" {
		contentType := spec.contentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition",
			fmt.Sprintf(`form-data; name="file"; filename="%s"`, spec.filename))
		header.Set("Content-Type", contentType)

		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, nil, err
		}
		if _, err := part.Write(spec.content); err != nil {
			return nil, nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, nil, err
	}

	request, err := http.NewRequest(http.MethodPost, b.base+"/api/materials", &body)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	for name, value := range spec.headers {
		request.Header[name] = []string{value}
	}

	return b.do(request)
}

// ---------------------------------------------------------------- runner ----

type outcome struct {
	id     string
	name   string
	failed []string
}

type runner struct {
	baseURL   string
	passwords map[string]string

	outcomes []*outcome
	current  *outcome
}

// begin opens a new acceptance criterion.
func (r *runner) begin(id, name string) *outcome {
	result := &outcome{id: id, name: name}
	r.outcomes = append(r.outcomes, result)
	r.current = result
	return result
}

// check records a failure when condition is false.
func (r *runner) check(condition bool, format string, args ...any) {
	if !condition {
		r.current.failed = append(r.current.failed, fmt.Sprintf(format, args...))
	}
}

// fail records an unconditional failure.
func (r *runner) fail(format string, args ...any) {
	r.current.failed = append(r.current.failed, fmt.Sprintf(format, args...))
}

func (r *runner) report() int {
	failed := 0

	fmt.Println()
	fmt.Println("acceptance results")
	fmt.Println(strings.Repeat("-", 72))

	for _, result := range r.outcomes {
		status := "PASS"
		if len(result.failed) > 0 {
			status = "FAIL"
			failed++
		}

		fmt.Printf("%s %-7s %s\n", result.id, status, result.name)

		for _, note := range result.failed {
			fmt.Printf("          - %s\n", note)
		}
	}

	fmt.Println(strings.Repeat("-", 72))
	fmt.Printf("%d checks, %d failed\n", len(r.outcomes), failed)

	return failed
}

// ------------------------------------------------------------- utilities ----

func (r *runner) login(username string) (*browser, bool) {
	session, err := newBrowser(username, r.baseURL)
	if err != nil {
		r.fail("create client: %v", err)
		return nil, false
	}

	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, r.passwords[username])

	response, payload, err := session.postJSON("/api/login", body)
	if err != nil {
		r.fail("login request failed: %v", err)
		return nil, false
	}

	if response.StatusCode != http.StatusOK {
		r.fail("login as %s: status %d (%s)", username, response.StatusCode, trim(payload))
		return nil, false
	}

	return session, true
}

type userJSON struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	ClassID   string `json:"class_id"`
	ClassName string `json:"class_name"`
}

type materialJSON struct {
	ID               string `json:"id"`
	ClassID          string `json:"class_id"`
	UploadedBy       string `json:"uploaded_by"`
	Title            string `json:"title"`
	OriginalFilename string `json:"original_filename"`
	ContentType      string `json:"content_type"`
}

func decodeUser(payload []byte) (userJSON, error) {
	var user userJSON
	err := json.Unmarshal(payload, &user)
	return user, err
}

func decodeWrappedMaterial(payload []byte) (materialJSON, error) {
	var body struct {
		Material materialJSON `json:"material"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return materialJSON{}, err
	}
	return body.Material, nil
}

func decodeDetail(payload []byte) (materialJSON, string, error) {
	var body struct {
		Material materialJSON `json:"material"`
		Content  string       `json:"content"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return materialJSON{}, "", err
	}
	return body.Material, body.Content, nil
}

func decodeList(payload []byte) ([]materialJSON, error) {
	var body struct {
		Materials []materialJSON `json:"materials"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, err
	}
	return body.Materials, nil
}

func decodeErrorCode(payload []byte) string {
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return ""
	}
	return body.Error.Code
}

func trim(payload []byte) string {
	const limit = 160

	text := strings.TrimSpace(string(payload))
	if len(text) > limit {
		return text[:limit] + "…"
	}
	return text
}

func ids(materials []materialJSON) []string {
	found := make([]string, 0, len(materials))
	for _, material := range materials {
		found = append(found, material.ID)
	}
	sort.Strings(found)
	return found
}
