package materials

import (
	"errors"
	"net/http"

	"campusclaw/internal/chunking"
	"campusclaw/internal/httpx"
)

// Upload failures are classified into these sentinels so the HTTP layer can
// map them onto the fixed status contract without inspecting error text.
var (
	// ErrUnsupportedType: an extension outside .md/.txt/.pdf/.docx.
	ErrUnsupportedType = errors.New("unsupported file type")
	// ErrInvalidFile: malformed, mislabelled or structurally broken content.
	ErrInvalidFile = errors.New("invalid file")
	// ErrTooLarge: a file, page, entry, decompressed or output size limit.
	ErrTooLarge = errors.New("content exceeds the allowed size")
	// ErrUnprocessable: encrypted, no extractable text, or a parse timeout.
	ErrUnprocessable = errors.New("content cannot be processed")
	// ErrParserBusy: the parse slot could not be acquired in time.
	ErrParserBusy = errors.New("document parser is busy")

	// ErrCommitUnknown: the transaction's outcome could not be confirmed. The
	// request fails, but the original file is never deleted on this path
	// because the rows may well have been committed.
	ErrCommitUnknown = errors.New("transaction result unknown")
)

// StatusForUpload maps an upload failure onto its fixed HTTP status and code.
// Anything unrecognised is an internal fault and never blamed on the file.
func StatusForUpload(err error) (int, string) {
	switch {
	case errors.Is(err, ErrUnsupportedType):
		return http.StatusUnsupportedMediaType, httpx.CodeUnsupportedMediaType
	case errors.Is(err, ErrInvalidFile), errors.Is(err, ErrUnsafeFilename):
		return http.StatusBadRequest, httpx.CodeBadRequest
	case errors.Is(err, chunking.ErrInvalidOptions):
		// An unusable split strategy is a bad request like any other bad form
		// field, and it is decided before anything is stored.
		return http.StatusBadRequest, httpx.CodeBadRequest
	case errors.Is(err, ErrTooLarge):
		return http.StatusRequestEntityTooLarge, httpx.CodePayloadTooLarge
	case errors.Is(err, ErrUnprocessable):
		return http.StatusUnprocessableEntity, httpx.CodeUnprocessableEntity
	case errors.Is(err, ErrParserBusy):
		return http.StatusServiceUnavailable, httpx.CodeServiceUnavailable
	default:
		return http.StatusInternalServerError, httpx.CodeInternal
	}
}
