package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
	"vizzel-backend/models"
)

// CreateDocument accepts multipart/form-data: project_id, doc_type, file.
// Uploads the file to Supabase Storage bucket "project-docs" then saves
// the record to the documents table.
func CreateDocument(c *gin.Context) {
	projectID := c.PostForm("project_id")
	docType := c.PostForm("doc_type")
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_id required"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	defer file.Close()

	ct := header.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}

	fileURL, err := uploadToStorage(projectID, header.Filename, ct, file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "upload failed: " + err.Error()})
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	var doc models.Document
	err = config.DB.QueryRow(context.Background(),
		`INSERT INTO documents (project_id, doc_type, file_url, uploaded_by)
		 VALUES ($1::uuid, $2, $3, NULLIF($4,'')::uuid)
		 RETURNING id, project_id::text, COALESCE(doc_type,''), file_url,
		           COALESCE(uploaded_by::text,''), created_at`,
		projectID, docType, fileURL, userIDStr,
	).Scan(&doc.ID, &doc.ProjectID, &doc.DocType, &doc.FileURL, &doc.UploadedBy, &doc.CreatedAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save document: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, doc)
}

// GetProjectDocuments lists all documents attached to a project.
func GetProjectDocuments(c *gin.Context) {
	projectID := c.Param("id")

	rows, err := config.DB.Query(context.Background(),
		`SELECT id, project_id::text, COALESCE(doc_type,''), file_url,
		        COALESCE(uploaded_by::text,''), created_at
		 FROM documents WHERE project_id = $1::uuid ORDER BY created_at DESC`,
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
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.DocType, &d.FileURL, &d.UploadedBy, &d.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse document"})
			return
		}
		docs = append(docs, d)
	}

	c.JSON(http.StatusOK, docs)
}

// uploadToStorage uploads a file to Supabase Storage and returns the public URL.
func uploadToStorage(projectID, filename, contentType string, r io.Reader) (string, error) {
	supabaseURL := os.Getenv("SUPABASE_URL")
	serviceKey := os.Getenv("SUPABASE_SERVICE_KEY")
	if supabaseURL == "" || serviceKey == "" {
		return "", fmt.Errorf("SUPABASE_URL or SUPABASE_SERVICE_KEY not configured")
	}

	ext := filepath.Ext(filename)
	objectPath := fmt.Sprintf("%s/%d%s", projectID, time.Now().UnixNano(), ext)
	uploadURL := fmt.Sprintf("%s/storage/v1/object/project-docs/%s", supabaseURL, objectPath)

	req, err := http.NewRequest(http.MethodPost, uploadURL, r)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+serviceKey)
	req.Header.Set("Content-Type", contentType)

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("storage %d: %s", resp.StatusCode, body)
	}

	return fmt.Sprintf("%s/storage/v1/object/public/project-docs/%s", supabaseURL, objectPath), nil
}
