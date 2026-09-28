package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"campusclaw/internal/materials"
)

func decodeCreatedMaterial(t *testing.T, recorder *httptest.ResponseRecorder) materialJSON {
	t.Helper()

	var body struct {
		Material materialJSON `json:"material"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode created material from %s: %v", recorder.Body.String(), err)
	}

	return body.Material
}

// AC05: a student is refused before the body is read, so neither a valid nor a
// malformed multipart can leave anything behind.
func TestStudentUploadIsRejectedBeforeAnyWrite(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	student := studentCookie(t, env, "student_a1")

	materialsBefore := env.CountRows(t, "materials")
	knowledgeBefore := env.CountRows(t, "knowledge_entries")
	filesBefore := env.countStoredFiles(t)

	valid := env.upload(t, student, uploadRequest{
		title: "Should not exist", filename: "notes.md", content: []byte("hello"),
	})
	if valid.Code != http.StatusForbidden {
		t.Errorf("valid multipart: status = %d, want 403", valid.Code)
	}

	malformed := env.upload(t, student, uploadRequest{
		omitTitle: true, filename: "notes.md", content: []byte("hello"),
	})
	if malformed.Code != http.StatusForbidden {
		t.Errorf("malformed multipart: status = %d, want 403", malformed.Code)
	}

	noFile := env.upload(t, student, uploadRequest{
		title: "x", omitFile: true,
	})
	if noFile.Code != http.StatusForbidden {
		t.Errorf("missing file part: status = %d, want 403", noFile.Code)
	}

	if got := env.CountRows(t, "materials"); got != materialsBefore {
		t.Errorf("materials = %d, want %d", got, materialsBefore)
	}
	if got := env.CountRows(t, "knowledge_entries"); got != knowledgeBefore {
		t.Errorf("knowledge_entries = %d, want %d", got, knowledgeBefore)
	}
	if got := env.countStoredFiles(t); got != filesBefore {
		t.Errorf("stored files = %d, want %d", got, filesBefore)
	}
}

// AC06: markdown keeps its bytes and its text verbatim.
func TestMarkdownUploadStoresOriginalAndKnowledge(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)
	content := "# Title\n\nSome **markdown** text.\n"

	recorder := env.upload(t, teacher, uploadRequest{
		title: "  Lecture One  ", filename: "lecture.md", content: []byte(content),
	})

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	created := decodeCreatedMaterial(t, recorder)

	if created.Title != "Lecture One" {
		t.Errorf("title = %q, want the trimmed value", created.Title)
	}
	if created.ContentType != materials.ContentTypeMarkdown {
		t.Errorf("content_type = %q", created.ContentType)
	}
	if created.ClassID == "" || created.UploadedBy == "" {
		t.Error("class_id or uploaded_by is empty")
	}

	if location := recorder.Header().Get("Location"); location != "/api/materials/"+created.ID {
		t.Errorf("Location = %q, want /api/materials/%s", location, created.ID)
	}

	// The stored original must match the uploaded bytes exactly.
	stored := storedFiles(t, env.Config.UploadDir)
	if len(stored) != 1 {
		t.Fatalf("stored %d files, want 1", len(stored))
	}
	onDisk, err := os.ReadFile(stored[0])
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if string(onDisk) != content {
		t.Errorf("stored bytes = %q, want the uploaded bytes", onDisk)
	}

	// Knowledge text is the original text.
	detail := env.get(t, "/api/materials/"+created.ID, teacher)
	var detailed detailBody
	if err := json.Unmarshal(detail.Body.Bytes(), &detailed); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detailed.Content != content {
		t.Errorf("knowledge content = %q, want the original text", detailed.Content)
	}

	if got := env.CountRows(t, "knowledge_entries"); got != 1 {
		t.Errorf("knowledge_entries = %d, want 1", got)
	}
}

// AC07: plain text behaves the same way with its own content type.
func TestTextUploadStoresOriginalAndKnowledge(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)
	content := "plain text\nsecond line\n"

	recorder := env.upload(t, teacher, uploadRequest{
		title: "Notes", filename: "notes.txt", content: []byte(content),
	})

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	created := decodeCreatedMaterial(t, recorder)
	if created.ContentType != materials.ContentTypeText {
		t.Errorf("content_type = %q, want %q", created.ContentType, materials.ContentTypeText)
	}

	detail := env.get(t, "/api/materials/"+created.ID, teacher)
	var detailed detailBody
	if err := json.Unmarshal(detail.Body.Bytes(), &detailed); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detailed.Content != content {
		t.Errorf("content = %q, want %q", detailed.Content, content)
	}
}

// AC08: an unsupported extension is refused and leaves nothing behind, even
// when the client claims a text content type.
func TestUnsupportedExtensionsLeaveNoTrace(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)

	for _, name := range []string{"handout.doc", "macro.docm", "archive.zip", "payload.exe"} {
		materialsBefore := env.CountRows(t, "materials")
		filesBefore := env.countStoredFiles(t)

		recorder := env.upload(t, teacher, uploadRequest{
			title:    "Unsupported",
			filename: name,
			content:  []byte("plain looking content"),
			partType: "text/plain",
		})

		if recorder.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%s: status = %d, want 415", name, recorder.Code)
		}

		if got := env.CountRows(t, "materials"); got != materialsBefore {
			t.Errorf("%s: materials = %d, want %d", name, got, materialsBefore)
		}
		if got := env.countStoredFiles(t); got != filesBefore {
			t.Errorf("%s: stored files = %d, want %d", name, got, filesBefore)
		}
	}
}

// The extension check is case-insensitive.
func TestExtensionMatchingIsCaseInsensitive(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)

	for _, name := range []string{"NOTES.MD", "Notes.Txt"} {
		recorder := env.upload(t, teacher, uploadRequest{
			title: "Upper", filename: name, content: []byte("content"),
		})

		if recorder.Code != http.StatusCreated {
			t.Errorf("%s: status = %d, want 201", name, recorder.Code)
		}
	}
}

// AC15: forged tenant fields in the form and query are ignored.
func TestForgedTenantFieldsAreIgnored(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)
	classBID := env.ClassID(t, "Class B")
	studentBID := env.UserID(t, "student_b1")

	recorder := env.upload(t, teacher, uploadRequest{
		title:    "Forged",
		filename: "notes.md",
		content:  []byte("content"),
		extraField: map[string]string{
			"class_id":    itoa64(classBID),
			"uploaded_by": itoa64(studentBID),
			"role":        "student",
		},
	})

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	created := decodeCreatedMaterial(t, recorder)

	classAID := env.ClassID(t, "Class A")
	teacherID := env.UserID(t, "teacher_a")

	if created.ClassID != itoa64(classAID) {
		t.Errorf("class_id = %q, want Class A (%d)", created.ClassID, classAID)
	}
	if created.UploadedBy != itoa64(teacherID) {
		t.Errorf("uploaded_by = %q, want Teacher A (%d)", created.UploadedBy, teacherID)
	}

	// Class B must have gained nothing.
	var classBMaterials int
	if err := env.DB.QueryRow(`
		SELECT COUNT(*) FROM materials m JOIN classes c ON c.id = m.class_id WHERE c.name = 'Class B'
	`).Scan(&classBMaterials); err != nil {
		t.Fatalf("count class B materials: %v", err)
	}
	if classBMaterials != 0 {
		t.Errorf("Class B has %d materials, want 0", classBMaterials)
	}

	// The file must live under Class A's directory.
	entries, err := os.ReadDir(env.Config.UploadDir)
	if err != nil {
		t.Fatalf("read upload root: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != itoa64(classAID) {
		t.Errorf("upload directories = %v, want only Class A", entries)
	}
}

// MA01: field and text validation.
func TestUploadInputValidation(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)

	cases := map[string]struct {
		request uploadRequest
		want    int
	}{
		"missing title": {
			request: uploadRequest{omitTitle: true, filename: "a.txt", content: []byte("x")},
			want:    http.StatusBadRequest,
		},
		"blank title": {
			request: uploadRequest{title: "   ", filename: "a.txt", content: []byte("x")},
			want:    http.StatusBadRequest,
		},
		"missing file": {
			request: uploadRequest{title: "t", omitFile: true},
			want:    http.StatusBadRequest,
		},
		"two files": {
			request: uploadRequest{title: "t", filename: "a.txt", content: []byte("x"), fileCount: 2},
			want:    http.StatusBadRequest,
		},
		"empty file": {
			request: uploadRequest{title: "t", filename: "a.txt", content: []byte{}},
			want:    http.StatusBadRequest,
		},
		"invalid utf-8": {
			request: uploadRequest{title: "t", filename: "a.txt", content: []byte{0xff, 0xfe, 0xfd}},
			want:    http.StatusBadRequest,
		},
		"nul byte": {
			request: uploadRequest{title: "t", filename: "a.txt", content: []byte("before\x00after")},
			want:    http.StatusBadRequest,
		},
		"png disguised as text": {
			request: uploadRequest{title: "t", filename: "a.txt",
				content: []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")},
			want: http.StatusBadRequest,
		},
	}

	for name, testCase := range cases {
		materialsBefore := env.CountRows(t, "materials")
		filesBefore := env.countStoredFiles(t)

		recorder := env.upload(t, teacher, testCase.request)

		if recorder.Code != testCase.want {
			t.Errorf("%s: status = %d, want %d (body %s)",
				name, recorder.Code, testCase.want, recorder.Body.String())
		}

		if got := env.CountRows(t, "materials"); got != materialsBefore {
			t.Errorf("%s: materials = %d, want %d", name, got, materialsBefore)
		}
		if got := env.countStoredFiles(t); got != filesBefore {
			t.Errorf("%s: stored files = %d, want %d", name, got, filesBefore)
		}
	}
}

// MA01: the 5 MiB file ceiling and the 6 MiB request ceiling.
func TestUploadSizeLimits(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)

	overFile := env.upload(t, teacher, uploadRequest{
		title:    "Too big",
		filename: "big.txt",
		content:  bytes.Repeat([]byte("a"), materials.MaxFileBytes+1),
	})
	if overFile.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("file over 5 MiB: status = %d, want 413", overFile.Code)
	}

	overRequest := env.upload(t, teacher, uploadRequest{
		title:    "Too big",
		filename: "big.txt",
		content:  bytes.Repeat([]byte("a"), materials.MaxRequestBytes+1024),
	})
	if overRequest.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("request over 6 MiB: status = %d, want 413", overRequest.Code)
	}

	if got := env.CountRows(t, "materials"); got != 0 {
		t.Errorf("materials = %d, want 0 after rejected uploads", got)
	}
	if got := env.countStoredFiles(t); got != 0 {
		t.Errorf("stored files = %d, want 0", got)
	}

	// Exactly at the limit is accepted.
	atLimit := env.upload(t, teacher, uploadRequest{
		title:    "At the limit",
		filename: "limit.txt",
		content:  bytes.Repeat([]byte("a"), materials.MaxFileBytes),
	})
	if atLimit.Code != http.StatusCreated {
		t.Errorf("exactly 5 MiB: status = %d, want 201", atLimit.Code)
	}
}

// MA02: a filename that could address another directory is refused.
func TestUploadRejectsDangerousFilenames(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)

	for _, name := range []string{
		"../escape.txt",
		"..\\escape.txt",
		"nested/../../escape.txt",
		"/etc/passwd",
		`C:\Windows\system32\evil.txt`,
		"C:relative.txt",
		"..",
	} {
		materialsBefore := env.CountRows(t, "materials")

		recorder := env.upload(t, teacher, uploadRequest{
			title: "Traversal", filename: name, content: []byte("payload"),
		})

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400", name, recorder.Code)
		}

		if got := env.CountRows(t, "materials"); got != materialsBefore {
			t.Errorf("%q: materials = %d, want %d", name, got, materialsBefore)
		}
	}

	// Nothing may have been written outside the upload root.
	for _, path := range storedFiles(t, env.Config.UploadDir) {
		if !strings.HasPrefix(path, env.Config.UploadDir) {
			t.Errorf("file written outside the upload root: %s", path)
		}
	}
}

// MA02: two uploads of the same name coexist, and the first is untouched.
func TestDuplicateFilenamesDoNotOverwrite(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)

	first := env.upload(t, teacher, uploadRequest{
		title: "First", filename: "notes.md", content: []byte("first content"),
	})
	second := env.upload(t, teacher, uploadRequest{
		title: "Second", filename: "notes.md", content: []byte("second content"),
	})

	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("statuses = %d and %d, want 201 both", first.Code, second.Code)
	}

	stored := storedFiles(t, env.Config.UploadDir)
	if len(stored) != 2 {
		t.Fatalf("stored %d files, want 2", len(stored))
	}

	if filepath.Base(stored[0]) == filepath.Base(stored[1]) {
		t.Error("both uploads produced the same stored name")
	}

	// Whatever order the directory walk returned, both payloads must survive.
	seen := map[string]bool{}
	for _, path := range stored {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		seen[string(content)] = true
	}

	if !seen["first content"] || !seen["second content"] {
		t.Errorf("stored payloads = %v, want both uploads preserved", seen)
	}
}

// MA06: a text file may legitimately start with bytes that look like another
// format's signature.
func TestLiteralTextPrefixesRemainValid(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)

	cases := map[string]string{
		"PK\x03\x04 zip-looking":   "PK\x03\x04 not really a zip",
		"MZ executable-looking":    "MZ this is a note about DOS",
		"%PDF- pdf-looking":        "%PDF- is how this note starts",
	}

	for name, content := range cases {
		recorder := env.upload(t, teacher, uploadRequest{
			title: name, filename: "note.txt", content: []byte(content),
		})

		if recorder.Code != http.StatusCreated {
			t.Errorf("%s: status = %d, want 201 (body %s)", name, recorder.Code, recorder.Body.String())
			continue
		}

		created := decodeCreatedMaterial(t, recorder)

		detail := env.get(t, "/api/materials/"+created.ID, teacher)
		var detailed detailBody
		if err := json.Unmarshal(detail.Body.Bytes(), &detailed); err != nil {
			t.Fatalf("%s: decode detail: %v", name, err)
		}

		if detailed.Content != content {
			t.Errorf("%s: content = %q, want %q", name, detailed.Content, content)
		}
	}
}

// AC25: a PDF keeps its bytes, yields its text layer, and downloads unchanged.
func TestPDFUploadAndExtraction(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)
	pdf := buildCJKPDF("CampusClaw PDF", "教学材料")

	recorder := env.upload(t, teacher, uploadRequest{
		title: "PDF material", filename: "lecture.pdf", content: pdf,
	})

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	created := decodeCreatedMaterial(t, recorder)
	if created.ContentType != materials.ContentTypePDF {
		t.Errorf("content_type = %q, want %q", created.ContentType, materials.ContentTypePDF)
	}

	student := studentCookie(t, env, "student_a1")

	detail := env.get(t, "/api/materials/"+created.ID, student)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d", detail.Code)
	}

	var detailed detailBody
	if err := json.Unmarshal(detail.Body.Bytes(), &detailed); err != nil {
		t.Fatalf("decode detail: %v", err)
	}

	if !strings.Contains(detailed.Content, "CampusClaw PDF") {
		t.Errorf("extracted text %q is missing the Latin text", detailed.Content)
	}
	if !strings.Contains(detailed.Content, "教学材料") {
		t.Errorf("extracted text %q is missing the Chinese text", detailed.Content)
	}

	download := env.get(t, "/api/materials/"+created.ID+"/file", student)
	if download.Code != http.StatusOK {
		t.Fatalf("download status = %d", download.Code)
	}
	if !bytes.Equal(download.Body.Bytes(), pdf) {
		t.Error("downloaded bytes differ from the uploaded PDF")
	}
	if got := download.Header().Get("Content-Type"); got != materials.ContentTypePDF {
		t.Errorf("download Content-Type = %q, want %q", got, materials.ContentTypePDF)
	}
}

// AC26: DOCX paragraphs and table cells survive extraction, in both dialects.
func TestDOCXUploadAndExtraction(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)
	student := studentCookie(t, env, "student_a1")

	body := paragraph("First paragraph") +
		paragraph("第二段") +
		"<w:tbl>" + tableRow("A1", "B1") + tableRow("A2", "B2") + "</w:tbl>"

	dialects := map[string]bool{
		"transitional": false,
		"strict":       true,
	}

	for name, strict := range dialects {
		docx := buildDOCX(t, docxOptions{bodyXML: body, strict: strict})

		recorder := env.upload(t, teacher, uploadRequest{
			title: "DOCX " + name, filename: name + ".docx", content: docx,
		})

		if recorder.Code != http.StatusCreated {
			t.Fatalf("%s: status = %d, body = %s", name, recorder.Code, recorder.Body.String())
		}

		created := decodeCreatedMaterial(t, recorder)
		if created.ContentType != materials.ContentTypeDOCX {
			t.Errorf("%s: content_type = %q", name, created.ContentType)
		}

		detail := env.get(t, "/api/materials/"+created.ID, student)
		var detailed detailBody
		if err := json.Unmarshal(detail.Body.Bytes(), &detailed); err != nil {
			t.Fatalf("%s: decode detail: %v", name, err)
		}

		content := detailed.Content

		for _, want := range []string{"First paragraph", "第二段", "A1", "B1", "A2", "B2"} {
			if !strings.Contains(content, want) {
				t.Errorf("%s: extracted text %q is missing %q", name, content, want)
			}
		}

		// Paragraph order is preserved and cells stay tab-separated.
		if strings.Index(content, "First paragraph") > strings.Index(content, "第二段") {
			t.Errorf("%s: paragraph order changed: %q", name, content)
		}
		if !strings.Contains(content, "A1\tB1") {
			t.Errorf("%s: table cells are not tab-separated: %q", name, content)
		}

		download := env.get(t, "/api/materials/"+created.ID+"/file", student)
		if download.Code != http.StatusOK {
			t.Fatalf("%s: download status = %d", name, download.Code)
		}
		if !bytes.Equal(download.Body.Bytes(), docx) {
			t.Errorf("%s: downloaded bytes differ from the uploaded DOCX", name)
		}
	}
}

// AC27: broken, encrypted and text-free documents are refused atomically.
func TestDocumentValidationFailuresAreAtomic(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)

	missingPart := buildDOCX(t, docxOptions{omitContentTypes: true})
	corruptXML := buildDOCX(t, docxOptions{corruptDocument: true})
	plainArchive := plainZIP(t, map[string]string{"notes.txt": "just a zip"})
	imageOnly := buildDOCX(t, docxOptions{bodyXML: ""})
	macroDoc := buildDOCX(t, docxOptions{bodyXML: paragraph("macro"), macro: true})
	externalMain := buildDOCX(t, docxOptions{
		bodyXML: paragraph("external"), documentTarget: "http://evil.example/doc.xml", externalMain: true,
	})

	cases := map[string]struct {
		filename string
		content  []byte
		want     int
	}{
		"corrupt pdf": {
			filename: "broken.pdf",
			content:  []byte("%PDF-1.4\nthis is not a real pdf body\n%%EOF"),
			want:     http.StatusBadRequest,
		},
		"plain zip as docx": {
			filename: "archive.docx",
			content:  plainArchive,
			want:     http.StatusBadRequest,
		},
		"docx missing content types": {
			filename: "noct.docx",
			content:  missingPart,
			want:     http.StatusBadRequest,
		},
		"docx with corrupt xml": {
			filename: "corrupt.docx",
			content:  corruptXML,
			want:     http.StatusBadRequest,
		},
		"macro enabled docx": {
			filename: "macro.docx",
			content:  macroDoc,
			want:     http.StatusBadRequest,
		},
		"external main document": {
			filename: "external.docx",
			content:  externalMain,
			want:     http.StatusBadRequest,
		},
		"ole container as docx": {
			filename: "legacy.docx",
			content:  oleContainer(),
			want:     http.StatusUnprocessableEntity,
		},
		"image only docx": {
			filename: "images.docx",
			content:  imageOnly,
			want:     http.StatusUnprocessableEntity,
		},
		"scanned pdf": {
			filename: "scan.pdf",
			content:  buildImageOnlyPDF(),
			want:     http.StatusUnprocessableEntity,
		},
	}

	for name, testCase := range cases {
		materialsBefore := env.CountRows(t, "materials")
		knowledgeBefore := env.CountRows(t, "knowledge_entries")
		filesBefore := env.countStoredFiles(t)

		recorder := env.upload(t, teacher, uploadRequest{
			title: name, filename: testCase.filename, content: testCase.content,
		})

		if recorder.Code != testCase.want {
			t.Errorf("%s: status = %d, want %d (body %s)",
				name, recorder.Code, testCase.want, recorder.Body.String())
		}

		if got := env.CountRows(t, "materials"); got != materialsBefore {
			t.Errorf("%s: materials = %d, want %d", name, got, materialsBefore)
		}
		if got := env.CountRows(t, "knowledge_entries"); got != knowledgeBefore {
			t.Errorf("%s: knowledge_entries = %d, want %d", name, got, knowledgeBefore)
		}
		// The saved original must have been cleaned up again.
		if got := env.countStoredFiles(t); got != filesBefore {
			t.Errorf("%s: stored files = %d, want %d", name, got, filesBefore)
		}
	}
}

// MA07: documents that exceed a parser bound are refused without side effects.
func TestParserBounds(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)

	deepBody := strings.Repeat(`<w:x>`, 200) + strings.Repeat(`</w:x>`, 200)

	cases := map[string]struct {
		filename string
		content  []byte
		want     int
	}{
		"too many zip entries": {
			filename: "many.docx",
			content:  buildDOCX(t, docxOptions{bodyXML: paragraph("x"), entryPad: 1100}),
			want:     http.StatusRequestEntityTooLarge,
		},
		"xml nested too deeply": {
			filename: "deep.docx",
			content:  buildDOCX(t, docxOptions{bodyXML: deepBody}),
			want:     http.StatusRequestEntityTooLarge,
		},
		"too many pdf pages": {
			filename: "pages.pdf",
			content:  buildManyPagePDF(201),
			want:     http.StatusRequestEntityTooLarge,
		},
		"zip path traversal": {
			filename: "slip.docx",
			content:  buildDOCX(t, docxOptions{bodyXML: paragraph("x"), traversalPart: true}),
			want:     http.StatusBadRequest,
		},
		"duplicate package part": {
			filename: "dupe.docx",
			content:  buildDOCX(t, docxOptions{bodyXML: paragraph("x"), duplicatePart: true}),
			want:     http.StatusBadRequest,
		},
	}

	for name, testCase := range cases {
		materialsBefore := env.CountRows(t, "materials")
		filesBefore := env.countStoredFiles(t)

		recorder := env.upload(t, teacher, uploadRequest{
			title: name, filename: testCase.filename, content: testCase.content,
		})

		if recorder.Code != testCase.want {
			t.Errorf("%s: status = %d, want %d (body %s)",
				name, recorder.Code, testCase.want, recorder.Body.String())
		}

		if got := env.CountRows(t, "materials"); got != materialsBefore {
			t.Errorf("%s: materials = %d, want %d", name, got, materialsBefore)
		}
		if got := env.countStoredFiles(t); got != filesBefore {
			t.Errorf("%s: stored files = %d, want %d", name, got, filesBefore)
		}
	}
}

// MA03: a failure to write the original leaves no rows at all.
func TestStorageWriteFailureLeavesNoRows(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)

	// Point the upload root beneath a regular file: creating the class
	// directory then fails exactly as it would on a full or read-only disk.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("prepare blocker: %v", err)
	}
	env.Config.UploadDir = filepath.Join(blocker, "uploads")

	recorder := env.upload(t, teacher, uploadRequest{
		title: "No storage", filename: "notes.md", content: []byte("content"),
	})

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", recorder.Code)
	}

	assertNoRows(t, env, "materials")
	assertNoRows(t, env, "knowledge_entries")

	if _, err := os.Stat(blocker); err != nil {
		t.Errorf("the blocker file disappeared: %v", err)
	}
}

// AC28: the new formats inherit every access control.
func TestNewFormatsRetainAccessControls(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)
	studentA := studentCookie(t, env, "student_a1")
	studentB := studentCookie(t, env, "student_b1")

	classBID := env.ClassID(t, "Class B")
	studentBID := env.UserID(t, "student_b1")

	uploaded := map[string]string{}

	for name, fixture := range map[string]struct {
		filename string
		content  []byte
	}{
		"pdf":  {"lecture.pdf", buildCJKPDF("Access control", "权限")},
		"docx": {"lecture.docx", buildDOCX(t, docxOptions{bodyXML: paragraph("Access control")})},
	} {
		recorder := env.upload(t, teacher, uploadRequest{
			title:      "Protected " + name,
			filename:   fixture.filename,
			content:    fixture.content,
			extraField: map[string]string{"class_id": itoa64(classBID), "uploaded_by": itoa64(studentBID)},
		})

		if recorder.Code != http.StatusCreated {
			t.Fatalf("%s: status = %d, body = %s", name, recorder.Code, recorder.Body.String())
		}

		uploaded[name] = decodeCreatedMaterial(t, recorder).ID
	}

	classAID := env.ClassID(t, "Class A")
	teacherID := env.UserID(t, "teacher_a")

	for name, id := range uploaded {
		// Ownership is the session's, not the form's.
		var classID, uploadedBy uint64
		if err := env.DB.QueryRow(
			"SELECT class_id, uploaded_by FROM materials WHERE id = ?", id,
		).Scan(&classID, &uploadedBy); err != nil {
			t.Fatalf("%s: read row: %v", name, err)
		}
		if classID != classAID || uploadedBy != teacherID {
			t.Errorf("%s: class/uploader = %d/%d, want %d/%d", name, classID, uploadedBy, classAID, teacherID)
		}

		// Class A can read and download.
		if recorder := env.get(t, "/api/materials/"+id, studentA); recorder.Code != http.StatusOK {
			t.Errorf("%s: Class A detail status = %d", name, recorder.Code)
		}
		if recorder := env.get(t, "/api/materials/"+id+"/file", studentA); recorder.Code != http.StatusOK {
			t.Errorf("%s: Class A download status = %d", name, recorder.Code)
		}

		// Class B cannot.
		if recorder := env.get(t, "/api/materials/"+id, studentB); recorder.Code != http.StatusNotFound {
			t.Errorf("%s: Class B detail status = %d, want 404", name, recorder.Code)
		}
		if recorder := env.get(t, "/api/materials/"+id+"/file", studentB); recorder.Code != http.StatusNotFound {
			t.Errorf("%s: Class B download status = %d, want 404", name, recorder.Code)
		}

		// Anonymous cannot.
		if recorder := env.get(t, "/api/materials/"+id, nil); recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s: anonymous detail status = %d, want 401", name, recorder.Code)
		}
	}

	// A student cannot upload either new format.
	for _, fixture := range []struct {
		filename string
		content  []byte
	}{
		{"sneaky.pdf", buildCJKPDF("Nope", "否")},
		{"sneaky.docx", buildDOCX(t, docxOptions{bodyXML: paragraph("Nope")})},
	} {
		recorder := env.upload(t, studentA, uploadRequest{
			title: "Student", filename: fixture.filename, content: fixture.content,
		})
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s: student upload status = %d, want 403", fixture.filename, recorder.Code)
		}
	}

	// Class B's list stays empty.
	list := env.get(t, "/api/materials", studentB)
	var listed listBody
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Materials) != 0 {
		t.Errorf("Class B saw %d materials, want 0", len(listed.Materials))
	}

	// The API exposes no static upload route.
	for _, path := range []string{"/uploads", "/uploads/1/whatever.pdf"} {
		recorder := env.get(t, path, nil)
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 (uploads are never served statically)", path, recorder.Code)
		}
	}
}

// AU03-AU05: the origin guard applies to uploads, after authentication.
func TestUploadOriginGuard(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)
	student := studentCookie(t, env, "student_a1")
	content := []byte("origin check")

	// Baseline: no browser context headers at all is allowed.
	if recorder := env.upload(t, teacher, uploadRequest{
		title: "Origin", filename: "notes.md", content: content,
	}); recorder.Code != http.StatusCreated {
		t.Fatalf("no headers: status = %d, want 201", recorder.Code)
	}

	// A cross-origin upload is refused with no side effects.
	filesBefore := env.countStoredFiles(t)
	materialsBefore := env.CountRows(t, "materials")

	crossOrigin := env.uploadWithHeaders(t, teacher, uploadRequest{
		title: "Origin", filename: "notes.md", content: content,
	}, map[string]string{"Origin": "http://evil.example"})

	if crossOrigin.Code != http.StatusForbidden {
		t.Errorf("cross-origin: status = %d, want 403", crossOrigin.Code)
	}
	if got := env.countStoredFiles(t); got != filesBefore {
		t.Errorf("cross-origin wrote %d files, want no change", got-filesBefore)
	}
	if got := env.CountRows(t, "materials"); got != materialsBefore {
		t.Errorf("cross-origin added %d rows", got-materialsBefore)
	}

	// Authentication still precedes the origin check.
	anonymous := env.uploadWithHeaders(t, nil, uploadRequest{
		title: "Origin", filename: "notes.md", content: content,
	}, map[string]string{"Origin": "http://evil.example"})

	if anonymous.Code != http.StatusUnauthorized {
		t.Errorf("anonymous cross-origin: status = %d, want 401", anonymous.Code)
	}

	// A student is refused for their role, not for the origin.
	studentCross := env.uploadWithHeaders(t, student, uploadRequest{
		title: "Origin", filename: "notes.md", content: content,
	}, map[string]string{"Origin": "http://evil.example"})

	if studentCross.Code != http.StatusForbidden {
		t.Errorf("student cross-origin: status = %d, want 403", studentCross.Code)
	}

	// A matching origin is accepted on its own.
	sameOrigin := env.uploadWithHeaders(t, teacher, uploadRequest{
		title: "Origin", filename: "notes.md", content: content,
	}, map[string]string{"Origin": env.Config.PublicOrigin.String()})

	if sameOrigin.Code != http.StatusCreated {
		t.Errorf("same origin: status = %d, want 201 (body %s)", sameOrigin.Code, sameOrigin.Body.String())
	}
}
