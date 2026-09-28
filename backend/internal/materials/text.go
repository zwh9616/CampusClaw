package materials

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	// MaxFileBytes is the per-file ceiling; Nginx enforces the same number.
	MaxFileBytes = 5 << 20
	// MaxRequestBytes is the whole multipart body ceiling.
	MaxRequestBytes = 6 << 20
	// maxTitleRunes matches the materials.title column.
	maxTitleRunes = 255
	// multipartMemory is how much of the form is buffered before spilling to disk.
	multipartMemory = 1 << 20

	ContentTypeMarkdown = "text/markdown"
	ContentTypeText     = "text/plain"
	ContentTypePDF      = "application/pdf"
	ContentTypeDOCX     = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
)

// Content type is assigned by the server from the extension. The client's own
// Content-Type is only a hint and is never trusted or stored.
var contentTypeByExtension = map[string]string{
	".md":   ContentTypeMarkdown,
	".txt":  ContentTypeText,
	".pdf":  ContentTypePDF,
	".docx": ContentTypeDOCX,
}

// ContentTypeForExtension returns the server-assigned content type, matching
// the extension case-insensitively.
func ContentTypeForExtension(filename string) (string, bool) {
	contentType, ok := contentTypeByExtension[strings.ToLower(filepath.Ext(filename))]
	return contentType, ok
}

// IsTextExtension reports whether the format is stored as its own text.
func IsTextExtension(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".md", ".txt":
		return true
	default:
		return false
	}
}

// ValidateTextContent applies the rules for .md and .txt: at least one byte,
// valid UTF-8 throughout, and no NUL.
//
// There is deliberately no magic-number check. A Markdown file may legitimately
// begin with "PK", "MZ" or "%PDF-"; rejecting those by prefix would refuse
// valid teaching material (MA06).
func ValidateTextContent(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("%w: the file is empty", ErrInvalidFile)
	}

	if !utf8.Valid(data) {
		return fmt.Errorf("%w: not valid UTF-8", ErrInvalidFile)
	}

	if bytes.IndexByte(data, 0) >= 0 {
		return fmt.Errorf("%w: contains a NUL byte", ErrInvalidFile)
	}

	return nil
}

// ValidateExtractedText applies the rules every extraction result must meet:
// valid UTF-8, no NUL, and at least one non-whitespace character.
func ValidateExtractedText(text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("%w: no extractable text", ErrUnprocessable)
	}

	if !utf8.ValidString(text) {
		return fmt.Errorf("%w: extraction produced invalid UTF-8", ErrInvalidFile)
	}

	if strings.ContainsRune(text, 0) {
		return fmt.Errorf("%w: extraction produced a NUL byte", ErrInvalidFile)
	}

	return nil
}

// ValidateTitle enforces the trimmed 1..255 character window.
func ValidateTitle(title string) (string, error) {
	trimmed := strings.TrimSpace(title)

	if trimmed == "" {
		return "", fmt.Errorf("%w: title is required", ErrInvalidFile)
	}

	if utf8.RuneCountInString(trimmed) > maxTitleRunes {
		return "", fmt.Errorf("%w: title is longer than %d characters", ErrInvalidFile, maxTitleRunes)
	}

	return trimmed, nil
}
