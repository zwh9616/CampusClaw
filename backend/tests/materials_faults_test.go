//go:build testhooks

// This file exercises the fault-injection seams declared in the materials
// package. Those seams only have setters in a `testhooks` build, so the tests
// that drive them are gated by the same tag: the default `go test ./...` build
// neither compiles nor can trigger them.

package tests

import (
	"errors"
	"net/http"
	"testing"

	"campusclaw/internal/materials"
)

// errInjected stands in for any internal failure the hooks simulate.
var errInjected = errors.New("injected failure for testing")

// AC18 and MA08: a failing knowledge insert rolls the material insert back and
// removes the stored original, for every format.
func TestKnowledgeInsertFailureRollsBackEverything(t *testing.T) {
	for _, phase := range []string{"markdown", "pdf", "docx"} {
		t.Run(phase, func(t *testing.T) {
			env := NewEnv(t)
			env.Seed(t)

			teacher := teacherCookie(t, env)

			materials.SetKnowledgeInsertHook(func() error { return errInjected })
			t.Cleanup(materials.ResetHooks)

			var content []byte
			var filename string

			switch phase {
			case "markdown":
				filename, content = "notes.md", []byte("rollback me")
			case "pdf":
				filename, content = "doc.pdf", buildCJKPDF("Rollback", "回滚")
			case "docx":
				filename, content = "doc.docx", buildDOCX(t, docxOptions{bodyXML: paragraph("Rollback")})
			}

			filesBefore := env.countStoredFiles(t)

			recorder := env.upload(t, teacher, uploadRequest{
				title: "Rollback", filename: filename, content: content,
			})

			if recorder.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500 (body %s)", recorder.Code, recorder.Body.String())
			}

			// The independent connection is what makes this a real check that
			// the material insert was rolled back, not merely invisible.
			assertNoRows(t, env, "materials")
			assertNoRows(t, env, "knowledge_entries")

			if got := env.countStoredFiles(t); got != filesBefore {
				t.Errorf("stored files = %d, want %d: the saved original must be removed", got, filesBefore)
			}
		})
	}
}

// MA03: when the compensating delete itself fails, the request is still
// reported as a failure — never silently upgraded to success.
func TestDeleteFailureStillReportsFailure(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)

	materials.SetKnowledgeInsertHook(func() error { return errInjected })
	materials.SetFailFileDeletes(true)
	t.Cleanup(materials.ResetHooks)

	recorder := env.upload(t, teacher, uploadRequest{
		title: "Cleanup fails", filename: "notes.md", content: []byte("content"),
	})

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500: a cleanup failure is never a success", recorder.Code)
	}

	assertNoRows(t, env, "materials")
	assertNoRows(t, env, "knowledge_entries")
}

// MA03: an unconfirmed commit keeps the original rather than deleting a file
// whose material may well exist.
func TestUnknownCommitKeepsTheStoredFile(t *testing.T) {
	env := NewEnv(t)
	env.Seed(t)

	teacher := teacherCookie(t, env)

	materials.SetUnknownCommit(true)
	t.Cleanup(materials.ResetHooks)

	filesBefore := env.countStoredFiles(t)

	recorder := env.upload(t, teacher, uploadRequest{
		title: "Unknown commit", filename: "notes.md", content: []byte("content"),
	})

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", recorder.Code)
	}

	// The transaction really did commit, so the rows exist and the file must
	// still be there: deleting it would orphan a material.
	if got := env.countStoredFiles(t); got != filesBefore+1 {
		t.Errorf("stored files = %d, want %d: a possibly-committed file must not be deleted",
			got, filesBefore+1)
	}

	assertRowCount(t, env, "materials", 1)
	assertRowCount(t, env, "knowledge_entries", 1)
}
