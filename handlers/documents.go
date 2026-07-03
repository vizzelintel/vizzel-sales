package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"vizzel-backend/config"
	"vizzel-backend/internal/storage"
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

// site_survey allows up to 3 uploads; attachment (supplementary) up to 5.
const siteSurveyLimit = 3
const attachmentLimit = 5

// roleMayUploadDocType enforces who may upload each document type.
func roleMayUploadDocType(role, docType string) bool {
	staff := role == "support" || role == "admin"
	switch docType {
	case "quotation_support", "tor_support", "closing":
		return staff
	case "quotation_dealer", "tor_dealer", "contract", "site_survey", "attachment":
		return true
	default:
		return false
	}
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
		"quotation_dealer":  true,
		"tor_support":       true,
		"tor_dealer":        true,
		"contract":          true,
		"closing":           true,
		"site_survey":       true,
		"attachment":        true,
	}
	if !validDocTypes[docType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ประเภทเอกสารไม่ถูกต้อง"})
		return
	}

	// Quick pre-check before reading file (final check under row lock below).
	var existingCount int
	_ = config.DB.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM documents WHERE project_id = $1::uuid AND doc_type = $2`,
		projectID, docType,
	).Scan(&existingCount)
	if singleUploadDocs[docType] && existingCount >= 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "เอกสารประเภทนี้มีอยู่แล้ว กรุณาลบก่อนแนบใหม่",
		})
		return
	}
	if docType == "site_survey" && existingCount >= siteSurveyLimit {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "แนบเอกสาร Site Survey ครบแล้ว (สูงสุด " + strconv.Itoa(siteSurveyLimit) + " ครั้ง)",
		})
		return
	}
	if docType == "attachment" && existingCount >= attachmentLimit {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "แนบเอกสารประกอบครบแล้ว (สูงสุด " + strconv.Itoa(attachmentLimit) + " ไฟล์)",
		})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	defer file.Close()

	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	var uploaderRole string
	_ = config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,'') FROM users WHERE id = $1::uuid`, userIDStr,
	).Scan(&uploaderRole)
	if !roleMayUploadDocType(uploaderRole, docType) {
		c.JSON(http.StatusForbidden, gin.H{"error": "คุณไม่มีสิทธิ์แนบเอกสารประเภทนี้"})
		return
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

	storageKey := storageObjectKey(projectID, header.Filename)
	storedKey, err := docStore.Upload(storageKey, mimeType, file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "upload failed: " + err.Error()})
		return
	}
	fileURL := storedKey

	ctx := context.Background()
	tx, err := config.DB.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start transaction"})
		return
	}
	defer tx.Rollback(ctx)

	// Lock project row so two simultaneous uploads of the same doc_type cannot both pass.
	if _, err = tx.Exec(ctx, `SELECT 1 FROM projects WHERE id = $1::uuid FOR UPDATE`, projectID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "project lock failed"})
		return
	}

	existingCount = 0
	_ = tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM documents WHERE project_id = $1::uuid AND doc_type = $2`,
		projectID, docType,
	).Scan(&existingCount)
	if singleUploadDocs[docType] && existingCount >= 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "เอกสารประเภทนี้มีอยู่แล้ว กรุณาลบก่อนแนบใหม่",
		})
		return
	}
	if docType == "site_survey" && existingCount >= siteSurveyLimit {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "แนบเอกสาร Site Survey ครบแล้ว (สูงสุด " + strconv.Itoa(siteSurveyLimit) + " ครั้ง)",
		})
		return
	}
	if docType == "attachment" && existingCount >= attachmentLimit {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "แนบเอกสารประกอบครบแล้ว (สูงสุด " + strconv.Itoa(attachmentLimit) + " ไฟล์)",
		})
		return
	}

	var doc models.Document
	err = tx.QueryRow(ctx,
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
		if strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key") {
			c.JSON(http.StatusConflict, gin.H{
				"error": "มีผู้แนบเอกสารประเภทนี้แล้ว กรุณารีเฟรชหน้า",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save document: " + err.Error()})
		return
	}
	if err = tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to commit document"})
		return
	}

	doc.FileURL = documentDownloadURL(doc.ID)

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

	// site_survey / attachment do not change pipeline status.
	if docType != "site_survey" && docType != "attachment" {
		autoAdvanceStatus(projectID, docType, userIDStr)
	}

	c.JSON(http.StatusCreated, doc)
	go SyncProjectToLarkByID(projectID)
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
		log.Printf("[STATUS] fetch project %s: %v\n", projectID, err)
		return
	}

	if currentStatus == "closed" || currentStatus == "reject" {
		return
	}

	ownerIsSupport := projectOwnerIsSupport(ctx, createdBy)
	docCounts := projectDocCounts(ctx, projectID)

	var nextStatus string
	switch docType {
	case "quotation_support", "quotation_dealer":
		if currentStatus == "present" || currentStatus == "register" {
			nextStatus = "quotation"
		}

	case "tor_support":
		if currentStatus == "quotation" {
			if ownerIsSupport {
				// Support-created project: one TOR doc is enough.
				nextStatus = "tor"
			} else {
				// Dealer-created project: also need tor_dealer.
				if docCounts["tor_dealer"] >= 1 {
					nextStatus = "tor"
				}
			}
		}

	case "tor_dealer":
		// Only advance when tor_support was already uploaded.
		if currentStatus == "quotation" && docCounts["tor_support"] >= 1 {
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
		log.Printf("[STATUS] advance failed project=%s newStatus=%s: %v\n", projectID, nextStatus, err)
		return
	}

	_, _ = config.DB.Exec(ctx,
		`INSERT INTO project_status_logs (project_id, from_status, to_status, changed_by, note)
		 VALUES ($1::uuid, $2, $3, NULLIF($4,'')::uuid, $5)`,
		projectID, currentStatus, nextStatus, userID,
		"อัปเดตอัตโนมัติจากการแนบเอกสาร: "+docType,
	)
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

	// Delete stored file (local or legacy Supabase URL)
	deleteStoredFile(fileURL)

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
	go SyncProjectToLarkByID(projectID)
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

	ownerIsSupport := projectOwnerIsSupport(ctx, createdBy)
	docCounts := projectDocCounts(ctx, projectID)
	quotationSupport := docCounts["quotation_support"]
	torSupport := docCounts["tor_support"]
	torDealer := docCounts["tor_dealer"]
	contractDocs := docCounts["contract"]
	closingDocs := docCounts["closing"]

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
		// If downstream doc gates are no longer met, roll back to register.
		if currentStatus == "quotation" || currentStatus == "tor" || currentStatus == "contract" || currentStatus == "closed" {
			newStatus = "register"
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
}

func projectOwnerIsSupport(ctx context.Context, createdBy string) bool {
	if createdBy == "" {
		return false
	}
	var role string
	if err := config.DB.QueryRow(ctx,
		`SELECT COALESCE(role,'') FROM users WHERE id = $1::uuid`, createdBy,
	).Scan(&role); err != nil {
		return false
	}
	return role == "support" || role == "admin"
}

func projectDocCounts(ctx context.Context, projectID string) map[string]int {
	counts := make(map[string]int)
	rows, err := config.DB.Query(ctx,
		`SELECT doc_type, COUNT(*)::int FROM documents WHERE project_id = $1::uuid GROUP BY doc_type`,
		projectID,
	)
	if err != nil {
		return counts
	}
	defer rows.Close()
	for rows.Next() {
		var typ string
		var n int
		if rows.Scan(&typ, &n) == nil {
			counts[typ] = n
		}
	}
	return counts
}

// deleteLegacySupabaseObject removes a file from Supabase Storage (pre-migration URLs).
func deleteLegacySupabaseObject(fileURL string) {
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

	for i := range docs {
		if !storage.IsLegacyRemoteURL(docs[i].FileURL) {
			docs[i].FileURL = documentDownloadURL(docs[i].ID)
		}
	}

	c.JSON(http.StatusOK, docs)
}

// DownloadDocument streams a document after JWT + project permission checks.
func DownloadDocument(c *gin.Context) {
	docID := c.Param("id")
	ctx := context.Background()
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	var projectID, fileURL, fileName, mimeType string
	err := config.DB.QueryRow(ctx, `
		SELECT d.project_id::text, d.file_url, COALESCE(d.file_name,''), COALESCE(d.mime_type,'')
		FROM documents d
		WHERE d.id = $1::uuid`, docID,
	).Scan(&projectID, &fileURL, &fileName, &mimeType)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch document"})
		}
		return
	}

	if !userCanAccessProject(ctx, userIDStr, projectID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "ไม่มีสิทธิ์เข้าถึงเอกสารนี้"})
		return
	}

	if storage.IsLegacyRemoteURL(fileURL) {
		c.Redirect(http.StatusTemporaryRedirect, fileURL)
		return
	}

	rc, err := docStore.Open(fileURL)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	defer rc.Close()

	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if fileName == "" {
		fileName = "document"
	}
	c.Header("Content-Type", mimeType)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, rc)
}

func userCanAccessProject(ctx context.Context, userID, projectID string) bool {
	var callerRole, callerCompanyID, projectCompanyID string
	if err := config.DB.QueryRow(ctx,
		`SELECT COALESCE(role,''), COALESCE(company_id::text,'')
		 FROM users WHERE id = $1::uuid`, userID,
	).Scan(&callerRole, &callerCompanyID); err != nil {
		return false
	}
	if callerRole == "admin" || callerRole == "support" {
		return true
	}
	if err := config.DB.QueryRow(ctx,
		`SELECT COALESCE(company_id::text,'') FROM projects WHERE id = $1::uuid`, projectID,
	).Scan(&projectCompanyID); err != nil {
		return false
	}
	if callerCompanyID != "" && callerCompanyID == projectCompanyID {
		return true
	}
	return false
}

func deleteStoredFile(fileURL string) {
	if storage.IsLegacyRemoteURL(fileURL) {
		deleteLegacySupabaseObject(fileURL)
		return
	}
	if docStore != nil {
		_ = docStore.Delete(fileURL)
	}
}

// storageObjectKey builds a safe ASCII path (Supabase rejects Thai/spaces in object keys).
func storageObjectKey(projectID, originalFilename string) string {
	ext := strings.ToLower(filepath.Ext(originalFilename))
	if ext == "" {
		ext = ".bin"
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return projectID + "/" + hex.EncodeToString(b) + ext
}

// encodeStoragePath is kept for legacy Supabase delete URLs.
func encodeStoragePath(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}
