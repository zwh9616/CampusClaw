package tests

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type materialJSON struct {
	ID               string `json:"id"`
	ClassID          string `json:"class_id"`
	UploadedBy       string `json:"uploaded_by"`
	Title            string `json:"title"`
	OriginalFilename string `json:"original_filename"`
	ContentType      string `json:"content_type"`
	CreatedAt        string `json:"created_at"`
}

type listBody struct {
	Materials []materialJSON `json:"materials"`
}

type detailBody struct {
	Material materialJSON `json:"material"`
	Content  string       `json:"content"`
}

// teacherCookie logs in Teacher A.
func teacherCookie(t *testing.T, env *Env) *http.Cookie {
	t.Helper()
	return env.Login(t, "teacher_a", env.Password(t, "teacher_a"))
}

// studentCookie logs in one of the two students.
func studentCookie(t *testing.T, env *Env, username string) *http.Cookie {
	t.Helper()
	return env.Login(t, username, env.Password(t, username))
}

// uploadMarkdown stores a markdown material as Teacher A and returns its id.
func uploadMarkdown(t *testing.T, env *Env, teacher *http.Cookie, title, text string) string {
	t.Helper()

	recorder := env.upload(t, teacher, uploadRequest{
		title:    title,
		filename: "notes.md",
		content:  []byte(text),
	})

	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload %q: status = %d, body = %s", title, recorder.Code, recorder.Body.String())
	}

	var body struct {
		Material materialJSON `json:"material"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}

	return body.Material.ID
}

func TestListIsEmptyArrayWhenClassHasNoMaterials(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	recorder := env.get(t, "/api/materials", studentCookie(t, env, "student_a1"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}

	// An empty list must be [], never null.
	if got := strings.TrimSpace(recorder.Body.String()); got != `{"materials":[]}` {
		t.Errorf("body = %s, want {\"materials\":[]}", got)
	}
}

// AC09: material becomes visible to a same-class student immediately.
func TestSameClassStudentSeesNewMaterialImmediately(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)
	materialID := uploadMarkdown(t, env, teacher, "Lecture 1", "# Heading\n\nBody text.")

	list := env.get(t, "/api/materials", studentCookie(t, env, "student_a1"))
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d", list.Code)
	}

	var listed listBody
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}

	if len(listed.Materials) != 1 {
		t.Fatalf("got %d materials, want 1", len(listed.Materials))
	}
	if listed.Materials[0].ID != materialID {
		t.Errorf("listed id = %q, want %q", listed.Materials[0].ID, materialID)
	}

	detail := env.get(t, "/api/materials/"+materialID, studentCookie(t, env, "student_a1"))
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d", detail.Code)
	}

	var detailed detailBody
	if err := json.Unmarshal(detail.Body.Bytes(), &detailed); err != nil {
		t.Fatalf("decode detail: %v", err)
	}

	if detailed.Content != "# Heading\n\nBody text." {
		t.Errorf("content = %q, want the original markdown", detailed.Content)
	}
}

// MAT-01: stored_filename and any disk path must never appear in a response.
func TestMaterialResponsesNeverExposeStorageDetails(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)
	materialID := uploadMarkdown(t, env, teacher, "Lecture", "text")

	for _, path := range []string{"/api/materials", "/api/materials/" + materialID} {
		recorder := env.get(t, path, teacher)

		var raw map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
			t.Fatalf("%s: decode: %v", path, err)
		}

		// The original filename is part of the contract; the stored name and
		// the directory it lives in are not.
		serialised := recorder.Body.String()
		for _, leak := range []string{"stored_filename", "storedFilename", env.Config.UploadDir} {
			if strings.Contains(serialised, leak) {
				t.Errorf("%s leaks %q: %s", path, leak, serialised)
			}
		}
	}
}

// AC10: another class sees nothing, and a forged class_id cannot change that.
func TestOtherClassListIsUnaffectedByForgedParameters(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)
	uploadMarkdown(t, env, teacher, "Class A material", "class A only")

	classAID := env.ClassID(t, "Class A")
	studentB := studentCookie(t, env, "student_b1")

	for _, path := range []string{
		"/api/materials",
		"/api/materials?class_id=" + itoa64(classAID),
		"/api/materials?class_id=Class%20A&role=teacher&uploaded_by=1",
	} {
		recorder := env.get(t, path, studentB)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", path, recorder.Code)
		}

		var listed listBody
		if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil {
			t.Fatalf("%s: decode: %v", path, err)
		}

		if len(listed.Materials) != 0 {
			t.Errorf("%s: Student B1 saw %d Class A materials", path, len(listed.Materials))
		}
	}
}

// AC11: detail for another class is byte-identical to detail for a missing id.
func TestOtherClassDetailIsIndistinguishableFromMissing(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)
	materialID := uploadMarkdown(t, env, teacher, "Class A material", "class A only")

	studentB := studentCookie(t, env, "student_b1")

	otherClass := env.get(t, "/api/materials/"+materialID, studentB)
	missing := env.get(t, "/api/materials/999999", studentB)

	if otherClass.Code != http.StatusNotFound {
		t.Errorf("other-class detail: status = %d, want 404", otherClass.Code)
	}
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing detail: status = %d, want 404", missing.Code)
	}

	if otherClass.Body.String() != missing.Body.String() {
		t.Errorf("bodies differ:\nother class: %s\nmissing:     %s",
			otherClass.Body.String(), missing.Body.String())
	}

	if otherClass.Header().Get("Content-Type") != missing.Header().Get("Content-Type") {
		t.Errorf("content types differ: %q vs %q",
			otherClass.Header().Get("Content-Type"), missing.Header().Get("Content-Type"))
	}
}

// MA05: every malformed id is answered exactly like a missing one.
func TestInvalidAndMissingIDsAreIndistinguishable(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)
	materialID := uploadMarkdown(t, env, teacher, "Lecture", "text")

	baseline := env.get(t, "/api/materials/999999", teacher)
	if baseline.Code != http.StatusNotFound {
		t.Fatalf("baseline status = %d, want 404", baseline.Code)
	}

	invalidIDs := []string{
		"abc", "0", "-1", "+1", "1.5", "1e3", "%201", "1%20",
		"18446744073709551616", "1_0", "0x10", "%E4%B8%AD",
	}

	for _, id := range invalidIDs {
		for _, suffix := range []string{"", "/file"} {
			path := "/api/materials/" + id + suffix
			recorder := env.get(t, path, teacher)

			if recorder.Code != http.StatusNotFound {
				t.Errorf("%s: status = %d, want 404", path, recorder.Code)
				continue
			}

			if recorder.Body.String() != baseline.Body.String() {
				t.Errorf("%s: body = %s, want %s", path, recorder.Body.String(), baseline.Body.String())
			}
		}
	}

	// A leading-zero form addresses the same row as its canonical form.
	leadingZeros := env.get(t, "/api/materials/00"+materialID, teacher)
	if leadingZeros.Code != http.StatusOK {
		t.Errorf("leading-zero id: status = %d, want 200", leadingZeros.Code)
	}
}

// MA04: the teacher can read their own class through all three GET endpoints.
func TestTeacherReadsOwnClass(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)
	materialID := uploadMarkdown(t, env, teacher, "Lecture", "hello")

	if recorder := env.get(t, "/api/materials", teacher); recorder.Code != http.StatusOK {
		t.Errorf("list: status = %d", recorder.Code)
	}

	detail := env.get(t, "/api/materials/"+materialID, teacher)
	if detail.Code != http.StatusOK {
		t.Errorf("detail: status = %d", detail.Code)
	}

	var detailed detailBody
	if err := json.Unmarshal(detail.Body.Bytes(), &detailed); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detailed.Content != "hello" {
		t.Errorf("content = %q", detailed.Content)
	}

	download := env.get(t, "/api/materials/"+materialID+"/file", teacher)
	if download.Code != http.StatusOK {
		t.Errorf("download: status = %d", download.Code)
	}
	if download.Body.String() != "hello" {
		t.Errorf("download = %q, want hello", download.Body.String())
	}
}

// AC04: the read endpoints all reject an anonymous caller.
func TestReadEndpointsRejectAnonymous(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	for _, path := range []string{"/api/me", "/api/materials", "/api/materials/1", "/api/materials/1/file"} {
		if recorder := env.get(t, path, nil); recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", path, recorder.Code)
		}
	}
}

func itoa64(value uint64) string {
	if value == 0 {
		return "0"
	}

	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
