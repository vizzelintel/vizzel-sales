package storage

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// FileStore persists project documents on disk or remote storage.
type FileStore interface {
	Upload(key, contentType string, r io.Reader) (storedKey string, err error)
	Open(key string) (io.ReadCloser, error)
	Delete(key string) error
}

// NewFromEnv returns the configured FileStore (local disk by default).
func NewFromEnv() FileStore {
	driver := strings.ToLower(strings.TrimSpace(os.Getenv("FILE_STORAGE_DRIVER")))
	if driver == "" || driver == "local" {
		base := strings.TrimSpace(os.Getenv("FILE_STORAGE_PATH"))
		if base == "" {
			base = "/data/docs"
		}
		return NewLocalFileStore(base)
	}
	return NewLocalFileStore("/data/docs")
}

// IsLegacyRemoteURL reports whether file_url points at Supabase or other HTTP storage.
func IsLegacyRemoteURL(fileURL string) bool {
	return strings.HasPrefix(strings.ToLower(fileURL), "http://") ||
		strings.HasPrefix(strings.ToLower(fileURL), "https://")
}

// NormalizeStoredKey returns the storage key for DB persistence.
func NormalizeStoredKey(key string) string {
	return strings.TrimPrefix(key, "local://")
}

// DisplayDownloadPath builds the authenticated download API path for clients.
func DisplayDownloadPath(documentID string) string {
	return fmt.Sprintf("/api/v1/documents/%s/download", documentID)
}
