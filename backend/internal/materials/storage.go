package materials

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"campusclaw/internal/httpx"
)

const (
	// classDirPerm and uploadFilePerm keep originals readable only by the API
	// process, never by the web server, which does not mount this volume.
	classDirPerm   os.FileMode = 0o700
	uploadFilePerm os.FileMode = 0o600
)

// Storage keeps uploaded originals in a private directory per class.
type Storage struct {
	root string
}

// NewStorage roots a storage area at root.
func NewStorage(root string) *Storage {
	return &Storage{root: root}
}

// classDir returns the class's directory, creating it when needed. The
// directory name is the numeric class id, which the application never takes
// from client input.
func (s *Storage) classDir(classID httpx.ID) (string, error) {
	dir := filepath.Join(s.root, strconv.FormatUint(uint64(classID), 10))

	if err := os.MkdirAll(dir, classDirPerm); err != nil {
		return "", fmt.Errorf("create class upload directory: %w", err)
	}

	return dir, nil
}

// Save writes the original bytes under a freshly generated name and returns
// that stored basename. Two uploads of the same filename get different names,
// so nothing is ever overwritten.
func (s *Storage) Save(classID httpx.ID, originalFilename string, content []byte) (string, error) {
	storedFilename, err := NewStoredFilename(originalFilename)
	if err != nil {
		return "", err
	}

	path, err := s.path(classID, storedFilename)
	if err != nil {
		return "", err
	}

	// O_EXCL turns a name collision into an error instead of a silent
	// overwrite, and refuses to follow a pre-existing symlink.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, uploadFilePerm)
	if err != nil {
		return "", fmt.Errorf("create upload file: %w", err)
	}

	if _, err := file.Write(content); err != nil {
		file.Close()
		os.Remove(path)
		return "", fmt.Errorf("write upload file: %w", err)
	}

	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("close upload file: %w", err)
	}

	return storedFilename, nil
}

// Open returns a handle to a stored original, after confirming the path stays
// inside the class directory and is not a symlink.
func (s *Storage) Open(classID httpx.ID, storedFilename string) (*os.File, error) {
	path, err := s.path(classID, storedFilename)
	if err != nil {
		return nil, err
	}

	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: %q is a symlink", ErrUnsafePath, storedFilename)
	}

	return os.Open(path)
}

// Path returns the absolute path of a stored original so an external parser
// can be handed a server-generated location rather than a client-supplied name.
func (s *Storage) Path(classID httpx.ID, storedFilename string) (string, error) {
	return s.path(classID, storedFilename)
}

// Delete removes a stored original. A file that is already gone is not an
// error, which keeps compensation after a failed upload idempotent.
func (s *Storage) Delete(classID httpx.ID, storedFilename string) error {
	path, err := s.path(classID, storedFilename)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete upload file: %w", err)
	}

	return nil
}

func (s *Storage) path(classID httpx.ID, storedFilename string) (string, error) {
	dir, err := s.classDir(classID)
	if err != nil {
		return "", err
	}

	return containWithin(dir, storedFilename)
}
