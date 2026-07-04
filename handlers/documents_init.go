package handlers

import (
	"vizzel-backend/internal/storage"
)

var docStore storage.FileStore

// InitDocumentStorage configures the document file backend from environment.
func InitDocumentStorage() {
	docStore = storage.NewFromEnv()
}

// documentDownloadURL returns the API path clients use to download a document securely.
func documentDownloadURL(documentID string) string {
	return storage.DisplayDownloadPath(documentID)
}
