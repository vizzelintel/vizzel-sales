package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"vizzel-backend/middleware"
	"vizzel-backend/models"
)

func CreateDocument(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.CreateDocumentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		lineUserID := middleware.GetLineUserID(c)

		doc := models.Document{
			ProjectID: req.ProjectID,
			Name:      req.Name,
			Type:      req.Type,
			URL:       req.URL,
			Size:      req.Size,
			CreatedBy: lineUserID,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}

		row := db.QueryRow(context.Background(),
			`INSERT INTO documents (project_id, name, type, url, size, created_by, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			 RETURNING id`,
			doc.ProjectID, doc.Name, doc.Type, doc.URL,
			doc.Size, doc.CreatedBy, doc.CreatedAt, doc.UpdatedAt,
		)

		if err := row.Scan(&doc.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create document"})
			return
		}

		c.JSON(http.StatusCreated, doc)
	}
}
