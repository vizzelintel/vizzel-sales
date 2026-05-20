package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"vizzel-backend/config"
	"vizzel-backend/models"
)

// Maximum uploads allowed per doc_type per project.
var docTypeLimits = map[string]int{
	"quotation_support": 1,
	"tor_support":       1,
	"tor_dealer":        1,
	"contract":          1,
	"closing":           1,
	"site_survey":       3,
}


// allowedExts maps accepted lowercase extensions to their canonical MIME type.
var allowedExts = map[string]string{
	".pdf":  "application/pdf",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
}

// CreateDocument accepts multipart/form-data: project_id, doc_type, file.
// Validates file type, enforces OTHER doc limit (max 5), uploads to Supabase
// Storage, and records file_name / file_size / mime_type in documents table.
func CreateDocument(c *gin.Context) {
	projectID := c.PostForm("project_id")
	docType := c.PostForm("doc_type")
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_id required"})
		return
	}

	validDocTypes := map[string]bool{
		"quotation_support": true,
		"tor_support":       true,
		"tor_dealer":        true,
		"contract":          true,
		"closing":           true,
		"site_survey":       true,
	}
	if !validDocTypes[docType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ประเภทเอกสารไม่ถูกต้อง"})
		return
	}

	// Enforce per-doc-type upload limit
	if limit, ok := docTypeLimits[docType]; ok {
		var count int
		if err := config.DB.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM documents WHERE project_id = $1::uuid AND doc_type = $2`,
			projectID, docType,
		).Scan(&count); err == nil && count >= limit {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "แนบเอกสารประเภทนี้ครบแล้ว (สูงสุด " + strconv.Itoa(limit) + " ครั้ง)",
			})
			return
		}
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	defer file.Close()

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	mimeType, ok := allowedExts[ext]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "รองรับเฉพาะไฟล์ PDF, Word และ Excel เท่านั้น"})
		return
	}
	// Prefer the browser-supplied Content-Type when reasonable, fall back to our map.
	if ct := header.Header.Get("Content-Type"); ct != "" && ct != "application/octet-stream" {
		mimeType = ct
	}

	fileURL, err := uploadToStorage(projectID, header.Filename, mimeType, file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "upload failed: " + err.Error()})
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	var doc models.Document
	err = config.DB.QueryRow(context.Background(),
		`INSERT INTO documents (project_id, doc_type, file_url, file_name, file_size, mime_type, uploaded_by)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6, NULLIF($7,'')::uuid)
		 RETURNING id, project_id::text, COALESCE(doc_type,''), file_url,
		           COALESCE(file_name,''), COALESCE(file_size,0), COALESCE(mime_type,''),
		           COALESCE(uploaded_by::text,''), created_at`,
		projectID, docType, fileURL, header.Filename, header.Size, mimeType, userIDStr,
	).Scan(
		&doc.ID, &doc.ProjectID, &doc.DocType, &doc.FileURL,
		&doc.FileName, &doc.FileSize, &doc.MimeType,
		&doc.UploadedBy, &doc.CreatedAt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save document: " + err.Error()})
		return
	}

	// Auto-advance project status based on uploaded doc type
	autoAdvanceStatus(projectID, docType, userIDStr)

	c.JSON(http.StatusCreated, doc)
}

// autoAdvanceStatus moves the project to the next sequential status when the
// correct document is uploaded. Called after every successful document insert.
func autoAdvanceStatus(projectID, docType, userID string) {
	ctx := context.Background()

	var currentStatus string
	if err := config.DB.QueryRow(ctx,
		`SELECT COALESCE(status,'') FROM projects WHERE id = $1::uuid`, projectID,
	).Scan(&currentStatus); err != nil {
		return
	}

	// Map (docType, currentStatus) → nextStatus
	var nextStatus string
	switch docType {
	case "quotation_support":
		// demo/site_survey are transient sub-activities that sit "on top of" present
		if currentStatus == "present" || currentStatus == "demo" || currentStatus == "site_survey" {
			nextStatus = "quotation"
		}
	case "tor_support":
		if currentStatus == "quotation" {
			nextStatus = "tor"
		}
	case "tor_dealer":
		// Advance to tor only when tor_support was also uploaded (both sides submitted)
		if currentStatus == "quotation" {
			var supportCount int
			if err := config.DB.QueryRow(ctx,
				`SELECT COUNT(*) FROM documents WHERE project_id = $1::uuid AND doc_type = 'tor_support'`,
				projectID,
			).Scan(&supportCount); err == nil && supportCount >= 1 {
				nextStatus = "tor"
			}
		}
	case "contract":
		if currentStatus == "tor" {
			nextStatus = "contract"
		}
	case "closing":
		if currentStatus == "contract" {
			nextStatus = "closed"
		}
	// site_survey docs: no status change
	}

	if nextStatus == "" {
		return
	}

	autoRejectExpr := `NOW() + INTERVAL '90 days'`
	if nextStatus == "contract" || nextStatus == "closed" {
		autoRejectExpr = `NULL`
	}

	if _, err := config.DB.Exec(ctx,
		fmt.Sprintf(`UPDATE projects
		 SET status           = $1,
		     last_activity_at = NOW(),
		     auto_reject_at   = %s
		 WHERE id = $2::uuid`, autoRejectExpr),
		nextStatus, projectID,
	); err != nil {
		return
	}

	// Log the auto-advance
	_, _ = config.DB.Exec(ctx,
		`INSERT INTO project_status_logs (project_id, from_status, to_status, changed_by, note)
		 VALUES ($1::uuid, $2, $3, NULLIF($4,'')::uuid, 'auto-advance on document upload')`,
		projectID, currentStatus, nextStatus, userID,
	)
}

// DeleteDocument removes a document record and its stored file.
// Returns 403 if the project is closed.
func DeleteDocument(c *gin.Context) {
	docID := c.Param("id")
	ctx   := context.Background()

	// Fetch document and its project status in one query
	var projectStatus, fileURL string
	err := config.DB.QueryRow(ctx, `
		SELECT p.status, d.file_url
		FROM documents d
		JOIN projects p ON p.id = d.project_id
		WHERE d.id = $1::uuid`, docID,
	).Scan(&projectStatus, &fileURL)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch document"})
		}
		return
	}

	if projectStatus == "closed" {
		c.JSON(http.StatusForbidden, gin.H{"error": "ไม่สามารถลบเอกสารได้ เนื่องจากงานปิดแล้ว"})
		return
	}

	// Delete from Supabase Storage (best-effort — DB deletion proceeds regardless)
	deleteFromStorage(fileURL)

	tag, err := config.DB.Exec(ctx, `DELETE FROM documents WHERE id = $1::uuid`, docID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete document"})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "ลบเอกสารสำเร็จ"})
}

// deleteFromStorage removes a file from Supabase Storage.
// The public URL format is: {BASE}/storage/v1/object/public/{bucket}/{file}
// The delete URL format is: {BASE}/storage/v1/object/{bucket}/{file}
func deleteFromStorage(fileURL string) {
	supabaseURL := os.Getenv("SUPABASE_URL")
	serviceKey  := os.Getenv("SUPABASE_SERVICE_KEY")
	if supabaseURL == "" || serviceKey == "" || fileURL == "" {
		return
	}
	deleteURL := strings.Replace(fileURL, "/storage/v1/object/public/", "/storage/v1/object/", 1)
	req, err := http.NewRequest(http.MethodDelete, deleteURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+serviceKey)
	(&http.Client{Timeout: 10 * time.Second}).Do(req) //nolint:errcheck
}

// GetProjectDocuments lists all documents for a project, oldest first (sequential display).
func GetProjectDocuments(c *gin.Context) {
	projectID := c.Param("id")

	rows, err := config.DB.Query(context.Background(),
		`SELECT id, project_id::text, COALESCE(doc_type,''), file_url,
		        COALESCE(file_name,''), COALESCE(file_size,0), COALESCE(mime_type,''),
		        COALESCE(uploaded_by::text,''), created_at
		 FROM documents WHERE project_id = $1::uuid ORDER BY created_at ASC`,
		projectID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch documents"})
		return
	}
	defer rows.Close()

	docs := make([]models.Document, 0)
	for rows.Next() {
		var d models.Document
		if err := rows.Scan(
			&d.ID, &d.ProjectID, &d.DocType, &d.FileURL,
			&d.FileName, &d.FileSize, &d.MimeType,
			&d.UploadedBy, &d.CreatedAt,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse document"})
			return
		}
		docs = append(docs, d)
	}

	c.JSON(http.StatusOK, docs)
}

// uploadToStorage uploads a file to Supabase Storage bucket "project-docs"
// and returns the public URL.
// Upload endpoint: POST {SUPABASE_URL}/storage/v1/object/project-docs/{path}
// Authorization:   Bearer {SUPABASE_SERVICE_KEY}
func uploadToStorage(projectID, filename, contentType string, r io.Reader) (string, error) {
	supabaseURL := os.Getenv("SUPABASE_URL")
	serviceKey := os.Getenv("SUPABASE_SERVICE_KEY")

	// Report each missing var by name to make misconfiguration obvious in logs.
	var missing []string
	if supabaseURL == "" {
		missing = append(missing, "SUPABASE_URL")
	}
	if serviceKey == "" {
		missing = append(missing, "SUPABASE_SERVICE_KEY")
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("env vars not set: %s", strings.Join(missing, ", "))
	}

	uploadURL := fmt.Sprintf("%s/storage/v1/object/project-docs/%s", supabaseURL, filename)

	req, err := http.NewRequest(http.MethodPost, uploadURL, r)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+serviceKey)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-upsert", "true")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("storage %d: %s", resp.StatusCode, body)
	}

	return fmt.Sprintf("%s/storage/v1/object/public/project-docs/%s", supabaseURL, filename), nil
}
