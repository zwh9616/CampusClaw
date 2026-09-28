package main

import (
	"bytes"
	"net/http"
	"strings"
)

// execute runs every acceptance criterion in order and returns once they have
// all been attempted. Each one records its own outcome, so a single failure
// never stops the run.
func (r *runner) execute() error {
	if err := r.checkHealth(); err != nil {
		return err
	}

	teacher, studentA, studentB, ok := r.signIn()
	if !ok {
		// Without a session nothing else can be attempted. Record it loudly so
		// a short report is never mistaken for a clean one.
		r.begin("AC00", "种子账号登录失败，其余验收未执行")
		r.fail("one or more seed accounts could not log in; every remaining check was skipped")
		return nil
	}

	r.checkAuthenticatedAPIs(studentB)
	r.checkRejectedLogins()
	r.checkTeacherLogin(teacher)
	r.checkStudentLogins(studentA, studentB)

	r.checkStudentUploadForbidden(studentA, teacher)
	r.checkUploadValidation(teacher)

	markdownID := r.checkTextUploads(teacher, studentA, studentB)

	r.checkClassIsolation(studentB, markdownID)
	r.checkDownloads(studentA, studentB, teacher, markdownID)
	r.checkDirectUploadPaths(studentB, teacher)
	r.checkForgedTenant(teacher)

	r.checkLogoutInvalidation()

	pdfID, docxID := r.checkDocumentUploads(teacher, studentA)
	r.checkDocumentFailures(teacher)
	r.checkDocumentAccessControl(studentA, studentB, pdfID, docxID)

	return nil
}

// signIn creates one browser per role and logs each in.
func (r *runner) signIn() (teacher, studentA, studentB *browser, ok bool) {
	teacher, teacherOK := r.login("teacher_a")
	studentA, studentAOK := r.login("student_a1")
	studentB, studentBOK := r.login("student_b1")

	return teacher, studentA, studentB, teacherOK && studentAOK && studentBOK
}

// AC21
func (r *runner) checkHealth() error {
	r.begin("AC21", "经 Nginx 的 /health 无需登录即返回 200")

	session, err := newBrowser("anonymous", r.baseURL)
	if err != nil {
		return err
	}

	response, payload, err := session.get("/health")
	if err != nil {
		r.fail("health request failed: %v", err)
		return nil
	}

	r.check(response.StatusCode == http.StatusOK, "status = %d, want 200", response.StatusCode)
	r.check(strings.Contains(string(payload), `"status":"ok"`), "body = %s", trim(payload))

	// The probe must not be answered by the SPA.
	r.check(strings.Contains(response.Header.Get("Content-Type"), "application/json"),
		"Content-Type = %q, want JSON", response.Header.Get("Content-Type"))

	return nil
}

// AC04
func (r *runner) checkAuthenticatedAPIs(studentB *browser) {
	r.begin("AC04", "未登录访问受保护 API 返回 401")

	anonymous, err := newBrowser("anonymous", r.baseURL)
	if err != nil {
		r.fail("create client: %v", err)
		return
	}

	for _, target := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/me"},
		{http.MethodGet, "/api/materials"},
		{http.MethodGet, "/api/materials/1"},
		{http.MethodGet, "/api/materials/1/file"},
		{http.MethodPost, "/api/logout"},
	} {
		var response *http.Response
		var payload []byte
		var err error

		if target.method == http.MethodPost {
			response, payload, err = anonymous.post(target.path)
		} else {
			response, payload, err = anonymous.get(target.path)
		}

		if err != nil {
			r.fail("%s %s: %v", target.method, target.path, err)
			continue
		}

		r.check(response.StatusCode == http.StatusUnauthorized,
			"%s %s: status = %d, want 401", target.method, target.path, response.StatusCode)
		r.check(decodeErrorCode(payload) == "unauthorized",
			"%s %s: code = %q, want unauthorized", target.method, target.path, decodeErrorCode(payload))
	}

	// A student's cookie must not help an anonymous client either.
	r.check(studentB != nil, "student B session was not established")
}

// AC03
func (r *runner) checkRejectedLogins() {
	r.begin("AC03", "错误密码与未知用户名返回相同 401")

	session, err := newBrowser("bad-login", r.baseURL)
	if err != nil {
		r.fail("create client: %v", err)
		return
	}

	wrongPassword, wrongPayload, err := session.postJSON("/api/login",
		`{"username":"teacher_a","password":"definitely-not-the-password"}`)
	if err != nil {
		r.fail("wrong-password request: %v", err)
		return
	}

	unknownUser, unknownPayload, err := session.postJSON("/api/login",
		`{"username":"nobody_at_all","password":"whatever"}`)
	if err != nil {
		r.fail("unknown-user request: %v", err)
		return
	}

	r.check(wrongPassword.StatusCode == http.StatusUnauthorized,
		"wrong password: status = %d, want 401", wrongPassword.StatusCode)
	r.check(unknownUser.StatusCode == http.StatusUnauthorized,
		"unknown user: status = %d, want 401", unknownUser.StatusCode)
	r.check(string(wrongPayload) == string(unknownPayload),
		"responses differ: %s vs %s", trim(wrongPayload), trim(unknownPayload))
}

// AC01
func (r *runner) checkTeacherLogin(teacher *browser) {
	r.begin("AC01", "Teacher A 登录后 /api/me 返回教师身份与 Class A")

	if teacher == nil {
		r.fail("teacher session was not established")
		return
	}

	response, payload, err := teacher.get("/api/me")
	if err != nil {
		r.fail("me request: %v", err)
		return
	}

	r.check(response.StatusCode == http.StatusOK, "status = %d, want 200", response.StatusCode)

	user, err := decodeUser(payload)
	if err != nil {
		r.fail("decode me: %v (%s)", err, trim(payload))
		return
	}

	r.check(user.Username == "teacher_a", "username = %q", user.Username)
	r.check(user.Role == "teacher", "role = %q, want teacher", user.Role)
	r.check(user.ClassName == "Class A", "class = %q, want Class A", user.ClassName)
	r.check(user.ID != "" && user.ClassID != "", "id/class_id must be present")
}

// AC02
func (r *runner) checkStudentLogins(studentA, studentB *browser) {
	r.begin("AC02", "Student A1 与 Student B1 登录后返回各自班级")

	for _, expected := range []struct {
		session  *browser
		username string
		class    string
	}{
		{studentA, "student_a1", "Class A"},
		{studentB, "student_b1", "Class B"},
	} {
		if expected.session == nil {
			r.fail("%s session was not established", expected.username)
			continue
		}

		response, payload, err := expected.session.get("/api/me")
		if err != nil {
			r.fail("%s: %v", expected.username, err)
			continue
		}

		r.check(response.StatusCode == http.StatusOK,
			"%s: status = %d, want 200", expected.username, response.StatusCode)

		user, err := decodeUser(payload)
		if err != nil {
			r.fail("%s: decode me: %v", expected.username, err)
			continue
		}

		r.check(user.Role == "student", "%s: role = %q, want student", expected.username, user.Role)
		r.check(user.ClassName == expected.class,
			"%s: class = %q, want %q", expected.username, user.ClassName, expected.class)
	}
}

// AC05
func (r *runner) checkStudentUploadForbidden(studentA, teacher *browser) {
	r.begin("AC05", "学生直接上传（合法与非法 multipart）均 403")

	if studentA == nil || teacher == nil {
		r.fail("sessions were not established")
		return
	}

	before := r.materialIDs(teacher)

	valid, _, err := studentA.upload(uploadSpec{
		title: "学生的材料", filename: "notes.md", content: []byte("should never be stored"),
	})
	if err != nil {
		r.fail("valid upload: %v", err)
		return
	}
	r.check(valid.StatusCode == http.StatusForbidden, "valid multipart: status = %d, want 403", valid.StatusCode)

	malformed, _, err := studentA.upload(uploadSpec{
		filename: "notes.md", content: []byte("no title"),
	})
	if err != nil {
		r.fail("malformed upload: %v", err)
		return
	}
	r.check(malformed.StatusCode == http.StatusForbidden,
		"malformed multipart: status = %d, want 403", malformed.StatusCode)

	r.checkUnchanged(before, teacher, "AC05")
}

// AC08 / MA01 / MA02
func (r *runner) checkUploadValidation(teacher *browser) {
	r.begin("AC08", "不支持的类型（.doc/.docm/.zip/.exe）返回 415 且无新增材料")

	if teacher == nil {
		r.fail("teacher session was not established")
		return
	}

	before := r.materialIDs(teacher)

	for _, name := range []string{"handout.doc", "macro.docm", "archive.zip", "payload.exe"} {
		response, _, err := teacher.upload(uploadSpec{
			title: "不支持的格式", filename: name,
			content: []byte("looks like text"), contentType: "text/plain",
		})
		if err != nil {
			r.fail("%s: %v", name, err)
			continue
		}

		r.check(response.StatusCode == http.StatusUnsupportedMediaType,
			"%s: status = %d, want 415", name, response.StatusCode)
	}

	r.checkUnchanged(before, teacher, "AC08")

	r.begin("AC-MA01", "非法内容与超限返回 400/413 且无新增材料")

	before = r.materialIDs(teacher)

	invalid := []struct {
		name    string
		spec    uploadSpec
		want    int
		explain string
	}{
		{"invalid utf-8", uploadSpec{
			title: "坏编码", filename: "bad.txt", content: []byte{0xff, 0xfe, 0xfd},
		}, http.StatusBadRequest, "invalid UTF-8"},
		{"nul byte", uploadSpec{
			title: "含 NUL", filename: "nul.txt", content: []byte("a\x00b"),
		}, http.StatusBadRequest, "NUL byte"},
		{"empty file", uploadSpec{
			title: "空文件", filename: "empty.txt", content: []byte{},
		}, http.StatusBadRequest, "empty file"},
		{"missing title", uploadSpec{
			filename: "notitle.txt", content: []byte("text"),
		}, http.StatusBadRequest, "missing title"},
		{"oversized", uploadSpec{
			title: "超大", filename: "big.txt", content: bytes.Repeat([]byte("a"), 5*1024*1024+1),
		}, http.StatusRequestEntityTooLarge, "over 5 MiB"},
	}

	for _, testCase := range invalid {
		response, _, err := teacher.upload(testCase.spec)
		if err != nil {
			r.fail("%s: %v", testCase.name, err)
			continue
		}

		r.check(response.StatusCode == testCase.want,
			"%s (%s): status = %d, want %d", testCase.name, testCase.explain, response.StatusCode, testCase.want)
	}

	r.checkUnchanged(before, teacher, "AC-MA01")

	r.begin("AC-MA02", "危险文件名返回 400，且不写到班级目录之外")

	before = r.materialIDs(teacher)

	for _, name := range []string{"../escape.txt", `..\escape.txt`, "/etc/passwd", `C:\Windows\evil.txt`} {
		response, _, err := teacher.upload(uploadSpec{
			title: "路径穿越", filename: name, content: []byte("payload"),
		})
		if err != nil {
			r.fail("%q: %v", name, err)
			continue
		}

		r.check(response.StatusCode == http.StatusBadRequest,
			"%q: status = %d, want 400", name, response.StatusCode)
	}

	r.checkUnchanged(before, teacher, "AC-MA02")
}

// AC06 / AC07 / AC09
func (r *runner) checkTextUploads(teacher, studentA, studentB *browser) string {
	r.begin("AC06", "上传 .md 后同班学生立即可见，知识内容为原文")

	if teacher == nil || studentA == nil {
		r.fail("sessions were not established")
		return ""
	}

	markdown := "# 验收标题\n\nAcceptance body text.\n"

	created, status, payload := r.uploadAndDecode(teacher, uploadSpec{
		title: "验收 Markdown", filename: "acceptance.md", content: []byte(markdown),
	})

	r.check(status == http.StatusCreated, "upload status = %d, want 201 (%s)", status, trim(payload))

	if status != http.StatusCreated {
		return ""
	}

	r.check(created.ContentType == "text/markdown", "content_type = %q", created.ContentType)
	r.check(created.Title == "验收 Markdown", "title = %q", created.Title)

	_, content, err := r.fetchDetail(studentA, created.ID)
	if err != nil {
		r.fail("student detail: %v", err)
	} else {
		r.check(content == markdown, "knowledge content = %q, want the original markdown", content)
	}

	r.begin("AC07", "上传 .txt 保留原文并归入本班")

	text := "plain acceptance text\nsecond line\n"

	textMaterial, status, payload := r.uploadAndDecode(teacher, uploadSpec{
		title: "验收文本", filename: "acceptance.txt", content: []byte(text),
	})
	r.check(status == http.StatusCreated, "upload status = %d, want 201 (%s)", status, trim(payload))

	if status == http.StatusCreated {
		r.check(textMaterial.ContentType == "text/plain", "content_type = %q", textMaterial.ContentType)

		_, content, err := r.fetchDetail(studentA, textMaterial.ID)
		if err != nil {
			r.fail("student detail: %v", err)
		} else {
			r.check(content == text, "content = %q, want %q", content, text)
		}
	}

	r.begin("AC09", "上传后同班学生在列表与详情中立即看到新材料的原文")

	list, err := r.materialIDsFor(studentA)
	if err != nil {
		r.fail("student list: %v", err)
		return created.ID
	}

	r.check(contains(list, created.ID), "student list %v is missing the new material %s", list, created.ID)

	if studentB != nil {
		otherList, err := r.materialIDsFor(studentB)
		if err != nil {
			r.fail("other-class list: %v", err)
		} else {
			r.check(!contains(otherList, created.ID),
				"Class B list %v must not contain Class A material %s", otherList, created.ID)
		}
	}

	return created.ID
}

// AC10 / AC11
func (r *runner) checkClassIsolation(studentB *browser, markdownID string) {
	r.begin("AC10", "B 班列表看不到 A 班材料，伪造 class_id 参数无效")

	if studentB == nil || markdownID == "" {
		r.fail("preconditions missing")
		return
	}

	for _, path := range []string{"/api/materials", "/api/materials?class_id=1", "/api/materials?class_id=Class%20A&role=teacher"} {
		response, payload, err := studentB.get(path)
		if err != nil {
			r.fail("%s: %v", path, err)
			continue
		}

		r.check(response.StatusCode == http.StatusOK, "%s: status = %d", path, response.StatusCode)

		materials, err := decodeList(payload)
		if err != nil {
			r.fail("%s: decode list: %v", path, err)
			continue
		}

		r.check(len(materials) == 0, "%s: Student B1 saw %d materials, want 0", path, len(materials))
	}

	r.begin("AC11", "B 班详情请求 A 班材料与不存在的 ID 返回相同 404")

	otherClass, otherPayload, err := studentB.get("/api/materials/" + markdownID)
	if err != nil {
		r.fail("other-class detail: %v", err)
		return
	}

	missing, missingPayload, err := studentB.get("/api/materials/999999")
	if err != nil {
		r.fail("missing detail: %v", err)
		return
	}

	r.check(otherClass.StatusCode == http.StatusNotFound,
		"other-class detail: status = %d, want 404", otherClass.StatusCode)
	r.check(missing.StatusCode == http.StatusNotFound,
		"missing detail: status = %d, want 404", missing.StatusCode)
	r.check(string(otherPayload) == string(missingPayload),
		"bodies differ: %s vs %s", trim(otherPayload), trim(missingPayload))

	// MA05: malformed ids are answered identically.
	for _, id := range []string{"abc", "0", "-1", "+1", "1.5", "1e3", "18446744073709551616"} {
		response, payload, err := studentB.get("/api/materials/" + id)
		if err != nil {
			r.fail("id %q: %v", id, err)
			continue
		}

		r.check(response.StatusCode == http.StatusNotFound, "id %q: status = %d, want 404", id, response.StatusCode)
		r.check(string(payload) == string(missingPayload), "id %q: body differs from the missing-id body", id)
	}
}

// AC12 / AC13 / MA04
func (r *runner) checkDownloads(studentA, studentB, teacher *browser, markdownID string) {
	r.begin("AC12", "B 班下载 A 班材料与不存在 ID 均返回相同 404")

	if studentB == nil || markdownID == "" {
		r.fail("preconditions missing")
		return
	}

	otherClass, otherPayload, err := studentB.get("/api/materials/" + markdownID + "/file")
	if err != nil {
		r.fail("other-class download: %v", err)
		return
	}

	missing, missingPayload, err := studentB.get("/api/materials/999999/file")
	if err != nil {
		r.fail("missing download: %v", err)
		return
	}

	r.check(otherClass.StatusCode == http.StatusNotFound,
		"other-class download: status = %d, want 404", otherClass.StatusCode)
	r.check(missing.StatusCode == http.StatusNotFound,
		"missing download: status = %d, want 404", missing.StatusCode)
	r.check(string(otherPayload) == string(missingPayload),
		"bodies differ: %s vs %s", trim(otherPayload), trim(missingPayload))

	r.begin("AC13", "同班学生下载原文件，字节与上传完全一致")

	if studentA == nil || teacher == nil {
		r.fail("sessions were not established")
		return
	}

	original := []byte("download byte-for-byte check\n")

	created, status, payload := r.uploadAndDecode(teacher, uploadSpec{
		title: "下载校验", filename: "download.md", content: original,
	})
	if status != http.StatusCreated {
		r.fail("upload status = %d (%s)", status, trim(payload))
		return
	}

	response, downloaded, err := studentA.get("/api/materials/" + created.ID + "/file")
	if err != nil {
		r.fail("download: %v", err)
		return
	}

	r.check(response.StatusCode == http.StatusOK, "download status = %d, want 200", response.StatusCode)
	r.check(bytes.Equal(downloaded, original), "downloaded bytes differ from the uploaded bytes")

	disposition := response.Header.Get("Content-Disposition")
	r.check(strings.HasPrefix(disposition, "attachment"), "Content-Disposition = %q, want attachment", disposition)
	r.check(response.Header.Get("X-Content-Type-Options") == "nosniff",
		"X-Content-Type-Options = %q, want nosniff", response.Header.Get("X-Content-Type-Options"))
	r.check(strings.Contains(response.Header.Get("Cache-Control"), "no-store"),
		"Cache-Control = %q, want no-store", response.Header.Get("Cache-Control"))

	r.begin("AC-MA04", "教师可读取本班的列表、详情与下载")

	for _, path := range []string{"/api/materials", "/api/materials/" + created.ID, "/api/materials/" + created.ID + "/file"} {
		response, _, err := teacher.get(path)
		if err != nil {
			r.fail("%s: %v", path, err)
			continue
		}
		r.check(response.StatusCode == http.StatusOK, "%s: status = %d, want 200", path, response.StatusCode)
	}
}

// AC14
func (r *runner) checkDirectUploadPaths(studentB, teacher *browser) {
	r.begin("AC14", "直接访问 /uploads 及其子路径一律 404")

	for _, session := range []*browser{studentB, teacher} {
		if session == nil {
			continue
		}

		for _, path := range []string{"/uploads", "/uploads/1", "/uploads/1/anything.md"} {
			response, payload, err := session.get(path)
			if err != nil {
				r.fail("%s: %v", path, err)
				continue
			}

			r.check(response.StatusCode == http.StatusNotFound,
				"%s: status = %d, want 404", path, response.StatusCode)

			// A 404 from nginx must not be the SPA's index.html.
			r.check(!strings.Contains(string(payload), "<div id=\"root\">"),
				"%s returned the SPA instead of a 404", path)
		}
	}
}

// AC15
func (r *runner) checkForgedTenant(teacher *browser) {
	r.begin("AC15", "上传时伪造 class_id/uploaded_by 不改变归属")

	if teacher == nil {
		r.fail("teacher session was not established")
		return
	}

	created, status, payload := r.uploadAndDecode(teacher, uploadSpec{
		title: "伪造归属", filename: "forged.md", content: []byte("forged tenant attempt"),
		fields: map[string]string{
			"class_id":    "999",
			"uploaded_by": "999",
			"role":        "student",
		},
	})

	r.check(status == http.StatusCreated, "upload status = %d, want 201 (%s)", status, trim(payload))
	if status != http.StatusCreated {
		return
	}

	_, mePayload, err := teacher.get("/api/me")
	if err != nil {
		r.fail("me request: %v", err)
		return
	}

	user, err := decodeUser(mePayload)
	if err != nil {
		r.fail("decode me: %v", err)
		return
	}

	r.check(created.ClassID == user.ClassID,
		"class_id = %q, want the session's %q", created.ClassID, user.ClassID)
	r.check(created.UploadedBy == user.ID,
		"uploaded_by = %q, want the session's %q", created.UploadedBy, user.ID)
}

// AC19
func (r *runner) checkLogoutInvalidation() {
	r.begin("AC19", "登出后旧 Session 失效")

	session, ok := r.login("student_a1")
	if !ok {
		return
	}

	response, _, err := session.post("/api/logout")
	if err != nil {
		r.fail("logout: %v", err)
		return
	}

	r.check(response.StatusCode == http.StatusNoContent, "logout status = %d, want 204", response.StatusCode)

	me, payload, err := session.get("/api/me")
	if err != nil {
		r.fail("me after logout: %v", err)
		return
	}

	r.check(me.StatusCode == http.StatusUnauthorized,
		"me after logout: status = %d, want 401", me.StatusCode)
	r.check(decodeErrorCode(payload) == "unauthorized", "code = %q", decodeErrorCode(payload))
}

// AC25 / AC26
func (r *runner) checkDocumentUploads(teacher, studentA *browser) (pdfID, docxID string) {
	r.begin("AC25", "上传 PDF：201、可提取中英文文本、下载字节一致")

	if teacher == nil || studentA == nil {
		r.fail("sessions were not established")
		return "", ""
	}

	pdf := buildPDF("CampusClaw PDF", "教学材料")

	pdfMaterial, status, payload := r.uploadAndDecode(teacher, uploadSpec{
		title: "验收 PDF", filename: "acceptance.pdf", content: pdf,
	})
	r.check(status == http.StatusCreated, "PDF upload status = %d, want 201 (%s)", status, trim(payload))

	if status == http.StatusCreated {
		pdfID = pdfMaterial.ID
		r.check(pdfMaterial.ContentType == "application/pdf", "content_type = %q", pdfMaterial.ContentType)

		_, content, err := r.fetchDetail(studentA, pdfID)
		if err != nil {
			r.fail("PDF detail: %v", err)
		} else {
			r.check(strings.Contains(content, "CampusClaw PDF"), "PDF text %q is missing the Latin text", content)
			r.check(strings.Contains(content, "教学材料"), "PDF text %q is missing the Chinese text", content)
		}

		response, downloaded, err := studentA.get("/api/materials/" + pdfID + "/file")
		if err != nil {
			r.fail("PDF download: %v", err)
		} else {
			r.check(response.StatusCode == http.StatusOK, "PDF download status = %d", response.StatusCode)
			r.check(bytes.Equal(downloaded, pdf), "downloaded PDF bytes differ from the uploaded PDF")
		}
	}

	r.begin("AC26", "上传 DOCX（Transitional 与 Strict）：段落与表格文字可提取")

	for _, dialect := range []struct {
		name   string
		strict bool
	}{
		{"Transitional", false},
		{"Strict", true},
	} {
		docx := buildCaptionDOCX(dialect.strict)

		material, status, payload := r.uploadAndDecode(teacher, uploadSpec{
			title: "验收 DOCX " + dialect.name, filename: dialect.name + ".docx", content: docx,
		})

		r.check(status == http.StatusCreated,
			"%s: upload status = %d, want 201 (%s)", dialect.name, status, trim(payload))

		if status != http.StatusCreated {
			continue
		}

		if dialect.name == "Transitional" {
			docxID = material.ID
		}

		r.check(material.ContentType ==
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			"%s: content_type = %q", dialect.name, material.ContentType)

		_, content, err := r.fetchDetail(studentA, material.ID)
		if err != nil {
			r.fail("%s: detail: %v", dialect.name, err)
		} else {
			for _, want := range []string{"Acceptance paragraph", "验收段落", "CellA1", "CellB1"} {
				r.check(strings.Contains(content, want),
					"%s: extracted text %q is missing %q", dialect.name, content, want)
			}
			r.check(strings.Contains(content, "CellA1\tCellB1"),
				"%s: table cells are not tab-separated: %q", dialect.name, content)
		}

		response, downloaded, err := studentA.get("/api/materials/" + material.ID + "/file")
		if err != nil {
			r.fail("%s: download: %v", dialect.name, err)
		} else {
			r.check(response.StatusCode == http.StatusOK, "%s: download status = %d", dialect.name, response.StatusCode)
			r.check(bytes.Equal(downloaded, docx), "%s: downloaded DOCX bytes differ", dialect.name)
		}
	}

	return pdfID, docxID
}

// AC27
func (r *runner) checkDocumentFailures(teacher *browser) {
	r.begin("AC27", "损坏/伪装/加密/无正文文档被拒绝且不留下记录")

	if teacher == nil {
		r.fail("teacher session was not established")
		return
	}

	before := r.materialIDs(teacher)

	cases := []struct {
		name    string
		spec    uploadSpec
		want    int
		explain string
	}{
		{"corrupt pdf", uploadSpec{
			title: "损坏 PDF", filename: "broken.pdf",
			content: []byte("%PDF-1.4\nnot a real document\n%%EOF"),
		}, http.StatusBadRequest, "corrupt"},
		{"plain zip as docx", uploadSpec{
			title: "伪装的 ZIP", filename: "archive.docx", content: buildPlainZIP(),
		}, http.StatusBadRequest, "not an OOXML package"},
		{"ole container as docx", uploadSpec{
			title: "OLE 容器", filename: "legacy.docx", content: buildOLEFile(),
		}, http.StatusUnprocessableEntity, "unsupported Office container"},
		{"image only docx", uploadSpec{
			title: "无正文 DOCX", filename: "images.docx", content: buildImageOnlyDOCX(),
		}, http.StatusUnprocessableEntity, "no extractable text"},
		{"scanned pdf", uploadSpec{
			title: "扫描 PDF", filename: "scan.pdf", content: buildScannedPDF(),
		}, http.StatusUnprocessableEntity, "no text layer"},
	}

	for _, testCase := range cases {
		response, payload, err := teacher.upload(testCase.spec)
		if err != nil {
			r.fail("%s: %v", testCase.name, err)
			continue
		}

		r.check(response.StatusCode == testCase.want,
			"%s (%s): status = %d, want %d (%s)",
			testCase.name, testCase.explain, response.StatusCode, testCase.want, trim(payload))
	}

	r.checkUnchanged(before, teacher, "AC27")
}

// AC28
func (r *runner) checkDocumentAccessControl(studentA, studentB *browser, pdfID, docxID string) {
	r.begin("AC28", "新格式继承全部访问控制")

	if studentA == nil || studentB == nil {
		r.fail("sessions were not established")
		return
	}

	for _, id := range []string{pdfID, docxID} {
		if id == "" {
			r.fail("a document upload did not succeed, so its access checks were skipped")
			continue
		}

		if response, _, err := studentA.get("/api/materials/" + id); err != nil {
			r.fail("Class A detail %s: %v", id, err)
		} else {
			r.check(response.StatusCode == http.StatusOK, "Class A detail %s: status = %d", id, response.StatusCode)
		}

		if response, _, err := studentB.get("/api/materials/" + id); err != nil {
			r.fail("Class B detail %s: %v", id, err)
		} else {
			r.check(response.StatusCode == http.StatusNotFound,
				"Class B detail %s: status = %d, want 404", id, response.StatusCode)
		}

		if response, _, err := studentB.get("/api/materials/" + id + "/file"); err != nil {
			r.fail("Class B download %s: %v", id, err)
		} else {
			r.check(response.StatusCode == http.StatusNotFound,
				"Class B download %s: status = %d, want 404", id, response.StatusCode)
		}

		list, err := r.materialIDsFor(studentB)
		if err != nil {
			r.fail("Class B list: %v", err)
		} else {
			r.check(!contains(list, id), "Class B list contains %s", id)
		}
	}

	// Students cannot upload the new formats either.
	for _, spec := range []uploadSpec{
		{title: "学生 PDF", filename: "sneaky.pdf", content: buildPDF("Nope", "否")},
		{title: "学生 DOCX", filename: "sneaky.docx", content: buildCaptionDOCX(false)},
	} {
		response, _, err := studentA.upload(spec)
		if err != nil {
			r.fail("%s: %v", spec.filename, err)
			continue
		}

		r.check(response.StatusCode == http.StatusForbidden,
			"student upload %s: status = %d, want 403", spec.filename, response.StatusCode)
	}
}

// ------------------------------------------------------------- helpers ------

func (r *runner) uploadAndDecode(session *browser, spec uploadSpec) (materialJSON, int, []byte) {
	response, payload, err := session.upload(spec)
	if err != nil {
		r.fail("%s: upload request failed: %v", spec.filename, err)
		return materialJSON{}, 0, nil
	}

	if response.StatusCode != http.StatusCreated {
		return materialJSON{}, response.StatusCode, payload
	}

	material, err := decodeWrappedMaterial(payload)
	if err != nil {
		r.fail("%s: decode created material: %v (%s)", spec.filename, err, trim(payload))
		return materialJSON{}, response.StatusCode, payload
	}

	return material, response.StatusCode, payload
}

func (r *runner) fetchDetail(session *browser, id string) (materialJSON, string, error) {
	response, payload, err := session.get("/api/materials/" + id)
	if err != nil {
		return materialJSON{}, "", err
	}

	if response.StatusCode != http.StatusOK {
		return materialJSON{}, "", errStatus(response.StatusCode)
	}

	return decodeDetail(payload)
}

func (r *runner) materialIDs(session *browser) []string {
	found, err := r.materialIDsFor(session)
	if err != nil {
		r.fail("read material list: %v", err)
		return nil
	}
	return found
}

func (r *runner) materialIDsFor(session *browser) ([]string, error) {
	response, payload, err := session.get("/api/materials")
	if err != nil {
		return nil, err
	}

	if response.StatusCode != http.StatusOK {
		return nil, errStatus(response.StatusCode)
	}

	materials, err := decodeList(payload)
	if err != nil {
		return nil, err
	}

	return ids(materials), nil
}

// checkUnchanged asserts the class list gained nothing.
func (r *runner) checkUnchanged(before []string, session *browser, label string) {
	after, err := r.materialIDsFor(session)
	if err != nil {
		r.fail("%s: read list after the rejected uploads: %v", label, err)
		return
	}

	r.check(len(after) == len(before),
		"%s: material count changed from %d to %d; a rejected upload left a record",
		label, len(before), len(after))
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type statusError int

func (e statusError) Error() string {
	return "unexpected status " + itoa(int(e))
}

func errStatus(code int) error { return statusError(code) }

func itoa(value int) string {
	if value == 0 {
		return "0"
	}

	digits := make([]byte, 0, 4)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
