package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalFileStore stores files under a base directory on disk.
type LocalFileStore struct {
	baseDir string
}

func NewLocalFileStore(baseDir string) *LocalFileStore {
	return &LocalFileStore{baseDir: filepath.Clean(baseDir)}
}

func (s *LocalFileStore) Upload(key, _ string, r io.Reader) (string, error) {
	key = NormalizeStoredKey(key)
	if err := validateKey(key); err != nil {
		return "", err
	}
	dest := filepath.Join(s.baseDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return key, nil
}

func (s *LocalFileStore) Open(key string) (io.ReadCloser, error) {
	key = NormalizeStoredKey(key)
	if err := validateKey(key); err != nil {
		return nil, err
	}
	path := filepath.Join(s.baseDir, filepath.FromSlash(key))
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	return f, nil
}

func (s *LocalFileStore) Delete(key string) error {
	key = NormalizeStoredKey(key)
	if err := validateKey(key); err != nil {
		return err
	}
	path := filepath.Join(s.baseDir, filepath.FromSlash(key))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete file: %w", err)
	}
	return nil
}

func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("empty storage key")
	}
	if strings.Contains(key, "..") {
		return fmt.Errorf("invalid storage key")
	}
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || strings.HasPrefix(clean, "..") {
		return fmt.Errorf("invalid storage key path")
	}
	return nil
}
