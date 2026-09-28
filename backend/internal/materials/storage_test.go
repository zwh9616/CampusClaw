package materials

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"campusclaw/internal/httpx"
)

func TestStorageSaveCreatesPrivateFileInsideClassDirectory(t *testing.T) {
	root := t.TempDir()
	storage := NewStorage(root)

	stored, err := storage.Save(httpx.ID(7), "notes.md", []byte("# hello"))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	classDir := filepath.Join(root, "7")

	dirInfo, err := os.Stat(classDir)
	if err != nil {
		t.Fatalf("class directory missing: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("class directory mode = %o, want 700", perm)
	}

	fileInfo, err := os.Stat(filepath.Join(classDir, stored))
	if err != nil {
		t.Fatalf("stored file missing: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("stored file mode = %o, want 600", perm)
	}

	content, err := os.ReadFile(filepath.Join(classDir, stored))
	if err != nil {
		t.Fatalf("read stored file: %v", err)
	}
	if string(content) != "# hello" {
		t.Errorf("stored content = %q, want the original bytes", content)
	}
}

// MA02: uploading the same filename twice must not overwrite the first file.
func TestStorageSaveKeepsBothCopiesOfADuplicateName(t *testing.T) {
	root := t.TempDir()
	storage := NewStorage(root)

	first, err := storage.Save(1, "notes.md", []byte("first"))
	if err != nil {
		t.Fatalf("first Save() error = %v", err)
	}

	second, err := storage.Save(1, "notes.md", []byte("second"))
	if err != nil {
		t.Fatalf("second Save() error = %v", err)
	}

	if first == second {
		t.Fatal("two uploads of the same filename produced the same stored name")
	}

	firstBytes, err := os.ReadFile(filepath.Join(root, "1", first))
	if err != nil {
		t.Fatalf("read first file: %v", err)
	}
	if string(firstBytes) != "first" {
		t.Errorf("first file = %q, want it unchanged", firstBytes)
	}

	secondBytes, _ := os.ReadFile(filepath.Join(root, "1", second))
	if string(secondBytes) != "second" {
		t.Errorf("second file = %q", secondBytes)
	}
}

func TestStorageOpenReturnsOriginalBytes(t *testing.T) {
	storage := NewStorage(t.TempDir())

	stored, err := storage.Save(1, "a.txt", []byte("payload"))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	handle, err := storage.Open(1, stored)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer handle.Close()

	content, err := io.ReadAll(handle)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(content) != "payload" {
		t.Errorf("content = %q, want payload", content)
	}
}

func TestStorageOpenRejectsEscapingNames(t *testing.T) {
	root := t.TempDir()
	storage := NewStorage(root)

	// A file that exists one level up must stay unreachable.
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	for _, name := range []string{"../secret.txt", `..\secret.txt`, "1/../../secret.txt", ""} {
		if _, err := storage.Open(1, name); err == nil {
			t.Errorf("Open(%q) succeeded, want an error", name)
		}
	}
}

func TestStorageDeleteIsIdempotent(t *testing.T) {
	root := t.TempDir()
	storage := NewStorage(root)

	stored, err := storage.Save(1, "a.txt", []byte("x"))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := storage.Delete(1, stored); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "1", stored)); !os.IsNotExist(err) {
		t.Errorf("file still exists after Delete()")
	}

	// Compensation runs after a failure; a second delete must not error.
	if err := storage.Delete(1, stored); err != nil {
		t.Errorf("second Delete() error = %v, want nil", err)
	}
}

func TestStorageDeleteRejectsEscapingNames(t *testing.T) {
	root := t.TempDir()
	storage := NewStorage(root)

	victim := filepath.Join(root, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	if err := storage.Delete(1, "../victim.txt"); err == nil {
		t.Error("Delete(../victim.txt) succeeded, want an error")
	}

	if _, err := os.Stat(victim); err != nil {
		t.Errorf("file outside the class directory was removed: %v", err)
	}
}

// Each class gets its own directory, so a stored name from one class cannot
// address another class's file.
func TestStorageKeepsClassesSeparate(t *testing.T) {
	root := t.TempDir()
	storage := NewStorage(root)

	stored, err := storage.Save(1, "a.txt", []byte("class one"))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := storage.Open(2, stored); err == nil {
		t.Error("class 2 opened a file stored for class 1")
	}
}

func TestStoragePathStaysInsideClassDirectory(t *testing.T) {
	root := t.TempDir()
	storage := NewStorage(root)

	stored, err := storage.Save(3, "a.txt", []byte("x"))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	path, err := storage.Path(3, stored)
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}

	want := filepath.Join(root, "3", stored)
	if path != want {
		t.Errorf("Path() = %q, want %q", path, want)
	}

	if _, err := storage.Path(3, "nested/a.txt"); !errors.Is(err, ErrUnsafePath) {
		t.Errorf("Path(nested/a.txt) error = %v, want ErrUnsafePath", err)
	}
}

func TestStorageDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	storage := NewStorage(root)

	target := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	classDir := filepath.Join(root, "1")
	if err := os.MkdirAll(classDir, 0o700); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	if err := os.Symlink(target, filepath.Join(classDir, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := storage.Open(1, "link.txt"); err == nil {
		t.Error("Open() followed a symlink, want an error")
	}
}

func TestStorageSaveRejectsUnsafeOriginalName(t *testing.T) {
	root := t.TempDir()
	storage := NewStorage(root)

	// The storage layer sanitizes rather than trusting the caller, so a
	// separator in the original name is neutralised, not honoured.
	stored, err := storage.Save(1, "../escape.txt", []byte("x"))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if strings.ContainsAny(stored, `/\`) {
		t.Errorf("stored name %q contains a separator", stored)
	}

	if _, err := os.Stat(filepath.Join(root, "escape.txt")); !os.IsNotExist(err) {
		t.Error("a file was written outside the class directory")
	}
}
