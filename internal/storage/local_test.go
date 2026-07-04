package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalFileStoreUploadOpenDelete(t *testing.T) {
	dir := t.TempDir()
	store := NewLocalFileStore(dir)

	key := "project-id/abc123.pdf"
	body := strings.NewReader("hello pdf")
	stored, err := store.Upload(key, "application/pdf", body)
	if err != nil {
		t.Fatal(err)
	}
	if stored != key {
		t.Fatalf("stored key = %q, want %q", stored, key)
	}

	rc, err := store.Open(key)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	n, _ := rc.Read(buf)
	rc.Close()
	if string(buf[:n]) != "hello pdf" {
		t.Fatalf("content = %q", string(buf[:n]))
	}

	if err := store.Delete(key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "project-id", "abc123.pdf")); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, err=%v", err)
	}
}

func TestValidateKeyRejectsTraversal(t *testing.T) {
	store := NewLocalFileStore(t.TempDir())
	_, err := store.Upload("../etc/passwd", "text/plain", strings.NewReader("x"))
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}
