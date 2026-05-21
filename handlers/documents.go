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

// singleUploadDocs: one upload per project (can be re-uploaded only after deletion).
var singleUploadDocs = map[string]bool{
	"quotation_support": true,
	"quotation_dealer":  true,
	"tor_support":       true,
	"tor_dealer":        true,
	"contract":          true,
	"closing":           true,
}

// site_survey allows up to 3 uploads.
const siteSurveyLimit = 3

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
		"quotation_dealer":  true,
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

	// Count existing docs of this type for this project
	var existingCount int
	_ = config.DB.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM documents WHERE project_id = $1::uuid AND doc_type = $2`,
		projectID, docType,
	).Scan(&existingCount)

	if singleUploadDocs[docType] {
		if existingCount >= 1 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "เอกสารประเภทนี้มีอยู่แล้ว กรุณาลบก่อนแนบใหม่",
			})
			return
		}
	} else if docType == "site_survey" {
		if existingCount >= siteSurveyLimit {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "แนบเอกสาร Site Survey ครบแล้ว (สูงสุด " + strconv.Itoa(siteSurveyLimit) + " ครั้ง)",
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

	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	// Only support/admin can upload closing document.
	if docType == "closing" {
		var uploaderRole string
		_ = config.DB.QueryRow(context.Background(),
			`SELECT COALESCE(role,'') FROM users WHERE id = $1::uuid`, userIDStr,
		).Scan(&uploaderRole)
		if uploaderRole != "support" && uploaderRole != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "เฉพาะ support/admin เท่านั้นที่ปิดงานได้"})
			return
		}
	}

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

	// Any upload is activity: reset 90-day timer for non-terminal statuses.
	_, _ = config.DB.Exec(context.Background(),
		`UPDATE projects
		 SET last_activity_at = NOW(),
		     auto_reject_at = CASE
		       WHEN status IN ('contract','closed','reject') THEN NULL
		       ELSE NOW() + INTERVAL '90 days'
		     END
		 WHERE id = $1::uuid`,
		projectID,
	)

	// site_survey is informational only; other document types may advance status.
	if docType != "site_survey" {
		autoAdvanceStatus(projectID, docType, userIDStr)
	}

	c.JSON(http.StatusCreated, doc)
}

// autoAdvanceStatus moves the project to the next sequential status when the
// correct document is uploaded. All errors are logged; none cause the upload to fail.
func autoAdvanceStatus(projectID, docType, userID string) {
	ctx := context.Background()

	// Fetch project status and the creator's UUID in one query.
	// created_by is the project owner; we use their role to decide TOR gating.
	var currentStatus, createdBy string
	if err := config.DB.QueryRow(ctx,
		`SELECT COALESCE(status,''), COALESCE(created_by::text,'')
		 FROM projects WHERE id = $1::uuid`, projectID,
	).Scan(&currentStatus, &createdBy); err != nil {
		fmt.Printf("[STATUS] failed to fetch project %s: %v\n", projectID, err)
		return
	}

	// Skip terminal states — nothing to advance.
	if currentStatus == "closed" || currentStatus == "reject" {
		fmt.Printf("[STATUS] project=%s already terminal (%s), skip\n", projectID, currentStatus)
		return
	}

	// Determine whether the project owner has a support/admin role.
	// Falls back to false (dealer) when role is empty/unset.
	ownerIsSupport := false
	if createdBy != "" {
		var ownerRole string
		if err := config.DB.QueryRow(ctx,
			`SELECT COALESCE(role,'') FROM users WHERE id = $1::uuid`, createdBy,
		).Scan(&ownerRole); err == nil {
			ownerIsSupport = ownerRole == "support" || ownerRole == "admin"
		}
	}

	countDocs := func(dtype string) int {
		var n int
		_ = config.DB.QueryRow(ctx,
			`SELECT COUNT(*) FROM documents WHERE project_id = $1::uuid AND doc_type = $2`,
			projectID, dtype,
		).Scan(&n)
		return n
	}

	var nextStatus string
	switch docType {
	case "quotation_support":
		if currentStatus == "present" {
			nextStatus = "quotation"
		}

	case "tor_support":
		if currentStatus == "quotation" {
			if ownerIsSupport {
				// Support-created project: one TOR doc is enough.
				nextStatus = "tor"
			} else {
				// Dealer-created project: also need tor_dealer.
				if countDocs("tor_dealer") >= 1 {
					nextStatus = "tor"
				}
			}
		}

	case "tor_dealer":
		// Only advance when tor_support was already uploaded.
		if currentStatus == "quotation" && countDocs("tor_support") >= 1 {
			nextStatus = "tor"
		}

	case "contract":
		if currentStatus == "tor" {
			nextStatus = "contract"
		}

	case "closing":
		if currentStatus == "contract" {
			nextStatus = "closed"
		}
		// site_survey: no status change
	}

	fmt.Printf("[STATUS] project=%s docType=%s ownerIsSupport=%v currentStatus=%s newStatus=%s\n",
		projectID, docType, ownerIsSupport, currentStatus, nextStatus)

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
		fmt.Printf("[STATUS] UPDATE failed project=%s newStatus=%s: %v\n", projectID, nextStatus, err)
		return
	}

	_, _ = config.DB.Exec(ctx,
		`INSERT INTO project_status_logs (project_id, from_status, to_status, changed_by, note)
		 VALUES ($1::uuid, $2, $3, NULLIF($4,'')::uuid, $5)`,
		projectID, currentStatus, nextStatus, userID,
		"อัปเดตอัตโนมัติจากการแนบเอกสาร: "+docType,
	)

	fmt.Printf("[STATUS] advanced project=%s %s→%s\n", projectID, currentStatus, nextStatus)

	// Sync updated project to Lark in background
	go func() {
		if p, err := scanProject(config.DB.QueryRow(ctx,
			`SELECT `+projectCols+` FROM projects WHERE id = $1::uuid`, projectID,
		)); err == nil {
			SyncProjectToLark(p)
		}
	}()
}

// DeleteDocument removes a document record and its stored file.
// Returns 403 if the project is closed.
func DeleteDocument(c *gin.Context) {
	docID := c.Param("id")
	ctx := context.Background()
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	// Fetch document and its project status in one query
	var projectStatus, fileURL, projectID, docType string
	err := config.DB.QueryRow(ctx, `
		SELECT p.status, d.file_url, d.project_id::text, COALESCE(d.doc_type,'')
		FROM documents d
		JOIN projects p ON p.id = d.project_id
		WHERE d.id = $1::uuid`, docID,
	).Scan(&projectStatus, &fileURL, &projectID, &docType)
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

	reconcileProjectStatusAfterDocumentDelete(projectID, docType, userIDStr)

	c.JSON(http.StatusOK, gin.H{"message": "ลบเอกสารสำเร็จ"})
}

func reconcileProjectStatusAfterDocumentDelete(projectID, deletedDocType, userID string) {
	ctx := context.Background()

	var currentStatus, createdBy string
	if err := config.DB.QueryRow(ctx,
		`SELECT COALESCE(status,''), COALESCE(created_by::text,'')
		 FROM projects WHERE id = $1::uuid`, projectID,
	).Scan(&currentStatus, &createdBy); err != nil {
		return
	}

	ownerIsSupport := false
	if createdBy != "" {
		var ownerRole string
		if err := config.DB.QueryRow(ctx,
			`SELECT COALESCE(role,'') FROM users WHERE id = $1::uuid`, createdBy,
		).Scan(&ownerRole); err == nil {
			ownerIsSupport = ownerRole == "support" || ownerRole == "admin"
		}
	}

	countDocs := func(dtype string) int {
		var n int
		_ = config.DB.QueryRow(ctx,
			`SELECT COUNT(*) FROM documents WHERE project_id = $1::uuid AND doc_type = $2`,
			projectID, dtype,
		).Scan(&n)
		return n
	}

	quotationSupport := countDocs("quotation_support")
	torSupport := countDocs("tor_support")
	torDealer := countDocs("tor_dealer")
	contractDocs := countDocs("contract")
	closingDocs := countDocs("closing")

	newStatus := currentStatus
	switch {
	case contractDocs > 0 && closingDocs > 0:
		newStatus = "closed"
	case contractDocs > 0:
		newStatus = "contract"
	case torSupport > 0 && (ownerIsSupport || torDealer > 0):
		newStatus = "tor"
	case quotationSupport > 0:
		newStatus = "quotation"
	default:
		// If downstream doc gates are no longer met, roll back to present.
		if currentStatus == "quotation" || currentStatus == "tor" || currentStatus == "contract" || currentStatus == "closed" {
			newStatus = "present"
		}
	}

	if newStatus == currentStatus {
		return
	}

	autoRejectExpr := `NOW() + INTERVAL '90 days'`
	if newStatus == "contract" || newStatus == "closed" || newStatus == "reject" {
		autoRejectExpr = `NULL`
	}

	if _, err := config.DB.Exec(ctx,
		fmt.Sprintf(`UPDATE projects
		 SET status = $1,
		     last_activity_at = NOW(),
		     auto_reject_at = %s
		 WHERE id = $2::uuid`, autoRejectExpr),
		newStatus, projectID,
	); err != nil {
		return
	}

	_, _ = config.DB.Exec(ctx,
		`INSERT INTO project_status_logs (project_id, from_status, to_status, changed_by, note)
		 VALUES ($1::uuid, $2, $3, NULLIF($4,'')::uuid, $5)`,
		projectID, currentStatus, newStatus, userID,
		"ปรับสถานะอัตโนมัติหลังลบเอกสาร: "+deletedDocType,
	)

	go func() {
		if p, err := scanProject(config.DB.QueryRow(ctx,
			`SELECT `+projectCols+` FROM projects WHERE id = $1::uuid`, projectID,
		)); err == nil {
			SyncProjectToLark(p)
		}
	}()
}

// deleteFromStorage removes a file from Supabase Storage.
// The public URL format is: {BASE}/storage/v1/object/public/{bucket}/{file}
// The delete URL format is: {BASE}/storage/v1/object/{bucket}/{file}
func deleteFromStorage(fileURL string) {
	supabaseURL := os.Getenv("SUPABASE_URL")
	serviceKey := os.Getenv("SUPABASE_SERVICE_KEY")
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
