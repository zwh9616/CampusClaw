package tests

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
)

// uploadRequest describes one multipart form posted to /api/materials.
type uploadRequest struct {
	title     string
	omitTitle bool

	filename string
	content  []byte
	// fileCount defaults to 1 when zero; set it to 0 explicitly with
	// omitFile to send no file part at all.
	fileCount  int
	omitFile   bool
	partType   string
	extraField map[string]string
}

// upload posts the form and returns the recorder.
//
// The file part is written with an explicit Content-Disposition so tests can
// send filenames containing separators, which is exactly what a hostile client
// would do.
func (e *Env) upload(t *testing.T, cookie *http.Cookie, request uploadRequest) *httptest.ResponseRecorder {
	t.Helper()
	return e.uploadWithHeaders(t, cookie, request, nil)
}

// uploadWithHeaders additionally sets request headers. Values are assigned
// through the map so a header that is present but empty stays distinguishable
// from an absent one.
func (e *Env) uploadWithHeaders(
	t *testing.T,
	cookie *http.Cookie,
	request uploadRequest,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()

	return e.Do(t, buildUploadRequest(t, cookie, request, headers))
}

// uploadVia sends an upload through a specific handler, which lets a test use
// a server built with substituted dependencies.
func (e *Env) uploadVia(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	request uploadRequest,
) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, buildUploadRequest(t, cookie, request, nil))
	return recorder
}

// buildUploadRequest assembles the multipart form without sending it.
func buildUploadRequest(
	t *testing.T,
	cookie *http.Cookie,
	request uploadRequest,
	headers map[string]string,
) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if !request.omitTitle {
		if err := writer.WriteField("title", request.title); err != nil {
			t.Fatalf("write title field: %v", err)
		}
	}

	for name, value := range request.extraField {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write field %s: %v", name, err)
		}
	}

	if !request.omitFile {
		count := request.fileCount
		if count == 0 {
			count = 1
		}

		for i := 0; i < count; i++ {
			writeFilePart(t, writer, "file", request.filename, request.partType, request.content)
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	httpRequest := httptest.NewRequest(http.MethodPost, "/api/materials", &body)
	httpRequest.Header.Set("Content-Type", writer.FormDataContentType())
	if cookie != nil {
		httpRequest.AddCookie(cookie)
	}
	for name, value := range headers {
		httpRequest.Header[name] = []string{value}
	}

	return httpRequest
}

func writeFilePart(t *testing.T, writer *multipart.Writer, field, filename, contentType string, content []byte) {
	t.Helper()

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, filename))
	header.Set("Content-Type", contentType)

	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}

	if _, err := part.Write(content); err != nil {
		t.Fatalf("write file part: %v", err)
	}
}

// countStoredFiles walks the upload root and counts stored originals.
func (e *Env) countStoredFiles(t *testing.T) int {
	t.Helper()

	return countFilesUnder(t, e.Config.UploadDir)
}
