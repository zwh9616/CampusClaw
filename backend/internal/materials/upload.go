package materials

import (
	"context"
	"errors"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"

	"campusclaw/internal/auth"
	"campusclaw/internal/httpx"
)

type createdResponse struct {
	Material Material `json:"material"`
}

// Upload accepts one teaching material, stores the original and records the
// material together with its extracted text.
//
// The caller is already authenticated and, via RequireTeacher, confirmed to be
// a teacher: both checks completed before a single byte of the body was read.
// The class and uploader come from the session and are never read from the
// form, so a forged class_id or uploaded_by has no effect.
func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFrom(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized)
		return
	}

	// Bound the request before any of it is buffered.
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBytes)

	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		writeUploadFailure(w, boundErr(err))
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	title, err := readTitle(r)
	if err != nil {
		writeUploadFailure(w, err)
		return
	}

	header, err := singleFileHeader(r)
	if err != nil {
		writeUploadFailure(w, err)
		return
	}

	// The name is checked before the extension so a hostile filename is
	// reported as a bad request rather than as an unsupported type. Validate
	// what the client actually wrote, not the basename the multipart reader
	// already reduced it to: without that, "../x.txt" would arrive as "x.txt"
	// and a traversal attempt would look like an ordinary upload.
	if err := ValidateOriginalFilename(sentFilename(header.Header, header.Filename)); err != nil {
		writeUploadFailure(w, err)
		return
	}

	contentType, supported := ContentTypeForExtension(header.Filename)
	if !supported {
		httpx.WriteError(w, http.StatusUnsupportedMediaType, httpx.CodeUnsupportedMediaType)
		return
	}

	content, err := readFile(header)
	if err != nil {
		writeUploadFailure(w, err)
		return
	}

	// Extension whitelisting already happened above; only the text formats
	// additionally require valid UTF-8 with no NUL. PDF and DOCX are binary
	// and are validated structurally by their extractors instead.
	if IsTextExtension(header.Filename) {
		if err := ValidateTextContent(content); err != nil {
			writeUploadFailure(w, err)
			return
		}
	}

	storedFilename, err := h.storage.Save(user.ClassID, header.Filename, content)
	if err != nil {
		log.Printf("materials: store upload failed: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal)
		return
	}

	text, err := h.extract(r.Context(), header.Filename, user.ClassID, storedFilename)
	if err != nil {
		h.discard(user.ClassID, storedFilename)
		writeUploadFailure(w, err)
		return
	}

	material, err := h.repo.Create(r.Context(), NewMaterial{
		ClassID:          user.ClassID,
		UploadedBy:       user.ID,
		Title:            title,
		OriginalFilename: header.Filename,
		StoredFilename:   storedFilename,
		ContentType:      contentType,
		Content:          text,
	})

	switch {
	case errors.Is(err, ErrCommitUnknown):
		// The rows may well have been committed, so the original is left in
		// place: deleting it could orphan a material that exists.
		log.Printf(
			"materials: commit outcome unknown for class %d; the stored original was kept and needs manual reconciliation",
			user.ClassID,
		)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal)
		return
	case err != nil:
		h.discard(user.ClassID, storedFilename)
		log.Printf("materials: create material failed: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal)
		return
	}

	w.Header().Set("Location", "/api/materials/"+material.ID.String())
	httpx.WriteJSON(w, http.StatusCreated, createdResponse{Material: material})
}

// extract reads the stored original through the extractor for its extension.
func (h *Handlers) extract(ctx context.Context, originalFilename string, classID httpx.ID, storedFilename string) (string, error) {
	extractor, ok := h.extractors[strings.ToLower(filepath.Ext(originalFilename))]
	if !ok {
		return "", ErrUnsupportedType
	}

	// Only the binary formats are expensive enough to queue for. A text file
	// is a read that needs no slot.
	if !IsTextExtension(originalFilename) {
		if err := h.limiter.Acquire(ctx); err != nil {
			return "", err
		}
		defer h.limiter.Release()
	}

	path, err := h.storage.Path(classID, storedFilename)
	if err != nil {
		return "", err
	}

	return extractor.Extract(ctx, path)
}

// discard removes a stored original after a failed upload. A failure to remove
// it is reported, never swallowed, and never turned into a success.
func (h *Handlers) discard(classID httpx.ID, storedFilename string) {
	err := h.storage.Delete(classID, storedFilename)
	if err == nil && failFileDeletes {
		err = errors.New("injected delete failure")
	}

	if err != nil {
		log.Printf(
			"materials: could not remove stored original %q for class %d after a failed upload; manual reconciliation required: %v",
			storedFilename, uint64(classID), err,
		)
	}
}

// sentFilename recovers the filename exactly as it appeared in
// Content-Disposition.
//
// mime/multipart deliberately strips any directory component from the filename
// it exposes, which is a sane default but hides traversal attempts from the
// validation that is supposed to reject them. Reading the header directly
// restores what the client sent.
func sentFilename(header textproto.MIMEHeader, fallback string) string {
	disposition := header.Get("Content-Disposition")
	if disposition == "" {
		return fallback
	}

	_, params, err := mime.ParseMediaType(disposition)
	if err != nil {
		return fallback
	}

	if name := params["filename"]; name != "" {
		return name
	}

	return fallback
}

func readTitle(r *http.Request) (string, error) {
	titles := r.MultipartForm.Value["title"]
	if len(titles) != 1 {
		return "", ErrInvalidFile
	}
	return ValidateTitle(titles[0])
}

func singleFileHeader(r *http.Request) (*multipart.FileHeader, error) {
	files := r.MultipartForm.File["file"]
	if len(files) != 1 {
		return nil, ErrInvalidFile
	}
	return files[0], nil
}

func readFile(header *multipart.FileHeader) ([]byte, error) {
	part, err := header.Open()
	if err != nil {
		return nil, ErrInvalidFile
	}
	defer part.Close()

	// One extra byte past the limit distinguishes "exactly at the limit" from
	// "over it" without buffering the whole oversized upload.
	content, err := io.ReadAll(io.LimitReader(part, MaxFileBytes+1))
	if err != nil {
		return nil, boundErr(err)
	}

	if len(content) > MaxFileBytes {
		return nil, ErrTooLarge
	}

	return content, nil
}

// boundErr converts the transport-level "request too large" error into the
// classification the API contract uses.
func boundErr(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return ErrTooLarge
	}
	return ErrInvalidFile
}

func writeUploadFailure(w http.ResponseWriter, err error) {
	status, code := StatusForUpload(err)
	httpx.WriteError(w, status, code)
}
