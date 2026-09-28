package materials

import (
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	// ErrUnsafeFilename means the client-supplied original name could be used
	// to write outside the class directory.
	ErrUnsafeFilename = errors.New("unsafe filename")
	// ErrUnsafePath means a stored name failed the containment check.
	ErrUnsafePath = errors.New("unsafe storage path")
)

const (
	// maxOriginalFilenameRunes matches the VARCHAR(255) column.
	maxOriginalFilenameRunes = 255
	// maxSafeFilenameBytes keeps uuid + "_" + safe name inside 255 bytes.
	maxSafeFilenameBytes = 180
	// maxExtensionBytes caps what is still recognisable as an extension.
	maxExtensionBytes = 20
)

// ValidateOriginalFilename rejects names that must never reach the filesystem.
//
// Separators are refused rather than stripped so a traversal attempt fails
// loudly at the edge instead of being silently rewritten into a different
// name the uploader did not choose.
func ValidateOriginalFilename(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty", ErrUnsafeFilename)
	}

	if utf8.RuneCountInString(name) > maxOriginalFilenameRunes {
		return fmt.Errorf("%w: longer than %d characters", ErrUnsafeFilename, maxOriginalFilenameRunes)
	}

	// A ".." *segment* is traversal; ".." inside a single component (for
	// example "notes..v2.md") is an ordinary filename and stays allowed.
	for _, segment := range strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return fmt.Errorf("%w: contains a parent-directory segment", ErrUnsafeFilename)
		}
	}

	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%w: contains a path separator", ErrUnsafeFilename)
	}

	if hasDriveLetter(name) {
		return fmt.Errorf("%w: looks like an absolute path", ErrUnsafeFilename)
	}

	for _, r := range name {
		if r == 0 || r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: contains a control character", ErrUnsafeFilename)
		}
	}

	return nil
}

func hasDriveLetter(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}

	first := name[0]
	return (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')
}

// SanitizeFilename maps an original name onto a safe basename: letters, digits,
// underscore, hyphen and dot survive and everything else becomes "_". The
// result is at most maxSafeFilenameBytes bytes, truncated on a rune boundary
// and keeping its extension.
func SanitizeFilename(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '_', r == '-', r == '.':
			return r
		case unicode.IsLetter(r), unicode.IsDigit(r):
			return r
		default:
			return '_'
		}
	}, name)

	extension := filepath.Ext(cleaned)
	if len(extension) > maxExtensionBytes {
		extension = ""
	}

	stem := strings.TrimSuffix(cleaned, extension)
	stem = truncateUTF8(stem, maxSafeFilenameBytes-len(extension))

	if stem == "" {
		stem = "file"
	}

	return stem + extension
}

// truncateUTF8 cuts s to at most maxBytes without splitting a rune.
func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}

	if len(s) <= maxBytes {
		return s
	}

	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return s[:cut]
}

// NewStoredFilename builds the on-disk name: a random UUID, an underscore and
// the sanitized original. The random prefix is what makes two uploads of the
// same filename collide-free, so an existing file is never overwritten.
func NewStoredFilename(originalFilename string) (string, error) {
	uuid, err := newUUID()
	if err != nil {
		return "", err
	}

	return uuid + "_" + SanitizeFilename(originalFilename), nil
}

// newUUID returns a random RFC 4122 version 4 identifier.
func newUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate uuid: %w", err)
	}

	raw[6] = (raw[6] & 0x0f) | 0x40 // version 4
	raw[8] = (raw[8] & 0x3f) | 0x80 // RFC 4122 variant

	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}

// containWithin joins a stored name onto a directory, refusing anything that is
// not a bare filename so a crafted value can never address another directory.
func containWithin(dir, storedFilename string) (string, error) {
	if storedFilename == "" || storedFilename == "." || storedFilename == ".." {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, storedFilename)
	}

	if strings.ContainsAny(storedFilename, `/\`) {
		return "", fmt.Errorf("%w: %q contains a separator", ErrUnsafePath, storedFilename)
	}

	if filepath.Base(storedFilename) != storedFilename {
		return "", fmt.Errorf("%w: %q is not a bare filename", ErrUnsafePath, storedFilename)
	}

	return filepath.Join(dir, storedFilename), nil
}
