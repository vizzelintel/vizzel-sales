package handlers

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
	"vizzel-backend/internal/storage"
)

func createDocumentMultipart(t *testing.T, projectID, docType, filename, content string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("project_id", projectID)
	_ = mw.WriteField("doc_type", docType)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/documents", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func countFiles(dir string) int {
	n := 0
	var walk func(d string)
	walk = func(d string) {
		items, _ := os.ReadDir(d)
		for _, e := range items {
			p := filepath.Join(d, e.Name())
			if e.IsDir() {
				walk(p)
			} else {
				n++
			}
		}
	}
	walk(dir)
	return n
}

// BUG-05: CreateDocument uploads the file to storage (docStore.Upload) before
// its second, row-locked limit re-check and before the INSERT. When two
// requests for the same single-upload doc type race past the first
// (unlocked) pre-check together, only one INSERT succeeds — the other
// request returns without ever deleting the file it already wrote to
// storage, leaving it orphaned. This fires several concurrent uploads for
// the same project/doc_type at once (a released start barrier maximizes the
// chance they overlap) and asserts storage never holds more files than the
// single accepted document row. See BUG_REPORT.md BUG-05.
func TestCreateDocument_DoesNotOrphanFileOnConcurrentSingleUpload(t *testing.T) {
	requireTestDB(t)
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	suffix := uniqueSuffix()

	tmpDir := t.TempDir()
	prevStore := docStore
	docStore = storage.NewLocalFileStore(tmpDir)
	t.Cleanup(func() { docStore = prevStore })

	companyID := testCompany(t, ctx, "Orphan Test Co "+suffix, "ORPH-"+suffix)
	dealerID := testUser(t, ctx, "line-orphan-"+suffix, "dealer", companyID)
	projectID := testProject(t, ctx, companyID, "Orphan Test Agency "+suffix)
	t.Cleanup(func() {
		config.DB.Exec(context.Background(), `DELETE FROM documents WHERE project_id = $1::uuid`, projectID)
	})

	const attempts = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = createDocumentMultipart(t, projectID, "contract",
				fmt.Sprintf("contract-%d.pdf", i), fmt.Sprintf("%%PDF-1.4 attempt %d", i))
			c.Set("user_id", dealerID)
			CreateDocument(c)
		}(i)
	}
	close(start)
	wg.Wait()

	var dbRowCount int
	if err := config.DB.QueryRow(ctx,
		`SELECT COUNT(*) FROM documents WHERE project_id = $1::uuid AND doc_type = 'contract'`, projectID,
	).Scan(&dbRowCount); err != nil {
		t.Fatalf("count document rows: %v", err)
	}
	if dbRowCount != 1 {
		t.Fatalf("expected exactly 1 document row to win the single-upload race, got %d", dbRowCount)
	}

	fileCount := countFiles(tmpDir)
	if fileCount != dbRowCount {
		t.Fatalf("storage holds %d file(s) but only %d document row(s) reference one — %d rejected "+
			"concurrent upload(s) left their file behind in storage instead of being deleted (BUG-05)",
			fileCount, dbRowCount, fileCount-dbRowCount)
	}
}
