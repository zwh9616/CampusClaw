package materials

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestValidateOriginalFilenameAcceptsOrdinaryNames(t *testing.T) {
	for _, name := range []string{
		"notes.md",
		"Lecture 01 - Intro.txt",
		"讲义-第1章.pdf",
		"a_b-c.docx",
		"2026.09.23 notes.txt",
		"..hidden-not-traversal",
	} {
		if err := ValidateOriginalFilename(name); err != nil {
			t.Errorf("ValidateOriginalFilename(%q) = %v, want nil", name, err)
		}
	}
}

func TestValidateOriginalFilenameRejectsTraversalAndPaths(t *testing.T) {
	cases := map[string]string{
		"empty":                 "",
		"unix traversal":        "../x.txt",
		"nested traversal":      "a/../../b.txt",
		"forward separator":     "dir/file.txt",
		"windows separator":     `dir\file.txt`,
		"windows traversal":     `..\..\x.txt`,
		"windows absolute":      `C:\Users\me\file.txt`,
		"drive relative":        "C:file.txt",
		"parent only":           "..",
		"nul byte":              "file\x00.txt",
		"newline":               "file\n.txt",
		"carriage return":       "file\r.txt",
		"escape":                "file\x1b.txt",
		"too many characters": strings.Repeat("x", maxOriginalFilenameRunes+1),
		"too many multibyte":  strings.Repeat("中", 300),
	}

	for name, value := range cases {
		if err := ValidateOriginalFilename(value); !errors.Is(err, ErrUnsafeFilename) {
			t.Errorf("%s: ValidateOriginalFilename(%q) = %v, want ErrUnsafeFilename", name, value, err)
		}
	}
}

func TestValidateOriginalFilenameAcceptsMaximumLength(t *testing.T) {
	name := strings.Repeat("x", maxOriginalFilenameRunes)

	if err := ValidateOriginalFilename(name); err != nil {
		t.Errorf("ValidateOriginalFilename at the limit = %v, want nil", err)
	}
}

func TestSanitizeFilenameReplacesUnsafeCharacters(t *testing.T) {
	cases := map[string]string{
		"plain":            "notes.md",
		"space":            "my notes.md",
		"unicode letters":  "讲义.md",
		"punctuation":      "a@b#c$.txt",
		"percent and plus": "100%+done.txt",
	}

	for name, original := range cases {
		got := SanitizeFilename(original)

		if strings.ContainsAny(got, `/\`) {
			t.Errorf("%s: SanitizeFilename(%q) = %q still has a separator", name, original, got)
		}
		if !utf8.ValidString(got) {
			t.Errorf("%s: SanitizeFilename(%q) = %q is not valid UTF-8", name, original, got)
		}
		if got == "" {
			t.Errorf("%s: SanitizeFilename(%q) is empty", name, original)
		}
	}
}

func TestSanitizeFilenameKeepsExtensionAndBoundsBytes(t *testing.T) {
	got := SanitizeFilename(strings.Repeat("中", 300) + ".md")

	if len(got) > maxSafeFilenameBytes {
		t.Errorf("length = %d bytes, want at most %d", len(got), maxSafeFilenameBytes)
	}
	if !strings.HasSuffix(got, ".md") {
		t.Errorf("SanitizeFilename(%q) = %q, want the .md extension kept", "中...md", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("truncation split a rune: %q", got)
	}
}

func TestSanitizeFilenameHandlesPathologicalInput(t *testing.T) {
	for _, original := range []string{"", ".", "..", "...", "/", "///", "中中中"} {
		got := SanitizeFilename(original)

		if got == "" {
			t.Errorf("SanitizeFilename(%q) is empty", original)
		}
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("SanitizeFilename(%q) = %q still has a separator", original, got)
		}
	}
}

func TestNewStoredFilenameIsUniqueAndBounded(t *testing.T) {
	const iterations = 64

	seen := make(map[string]bool, iterations)

	for i := 0; i < iterations; i++ {
		name, err := NewStoredFilename("lecture notes.md")
		if err != nil {
			t.Fatalf("NewStoredFilename() error = %v", err)
		}

		// MA02: the same original name must produce a different stored name.
		if seen[name] {
			t.Fatalf("NewStoredFilename() repeated %q", name)
		}
		seen[name] = true

		if len(name) > 255 {
			t.Errorf("stored name is %d bytes, want at most 255", len(name))
		}
		if !strings.HasSuffix(name, "_lecture_notes.md") {
			t.Errorf("stored name %q does not end with the sanitized original", name)
		}
	}
}

func TestContainWithinRejectsEscapes(t *testing.T) {
	for _, stored := range []string{"", ".", "..", "../x", `..\x`, "a/b", `a\b`} {
		if _, err := containWithin("/uploads/1", stored); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("containWithin(%q) = %v, want ErrUnsafePath", stored, err)
		}
	}

	got, err := containWithin("/uploads/1", "abc_notes.md")
	if err != nil {
		t.Fatalf("containWithin(valid) error = %v", err)
	}
	if got != "/uploads/1/abc_notes.md" {
		t.Errorf("containWithin(valid) = %q", got)
	}
}
