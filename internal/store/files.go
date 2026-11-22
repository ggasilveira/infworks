package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

// FileStore keeps uploaded PDFs under Dir with random names.
type FileStore struct{ Dir string }

var storedName = regexp.MustCompile(`^[0-9a-f]{32}\.pdf$`)

func NewFileStore(dir string) (*FileStore, error) {
	return &FileStore{Dir: dir}, os.MkdirAll(dir, 0o755)
}

func (f *FileStore) Save(data []byte) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	name := hex.EncodeToString(b) + ".pdf"
	return name, os.WriteFile(filepath.Join(f.Dir, name), data, 0o644)
}

func (f *FileStore) Remove(name string) error {
	p, err := f.Path(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// Path resolves a stored name, rejecting anything that is not one of ours.
func (f *FileStore) Path(name string) (string, error) {
	if !storedName.MatchString(name) {
		return "", errors.New("invalid document name")
	}
	return filepath.Join(f.Dir, name), nil
}
