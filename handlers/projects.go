package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
	"vizzel-backend/models"
)

func CreateProject(c *gin.Context) {
	var req models.CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	lineUserID := c.GetString("line_id")

	project := models.Project{
		CompanyID:   req.CompanyID,
		Name:        req.Name,
		Description: req.Description,
		Status:      "active",
		CreatedBy:   lineUserID,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	row := config.DB.QueryRow(context.Background(),
		`INSERT INTO projects (company_id, name, description, status, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id`,
		project.CompanyID, project.Name, project.Description,
		project.Status, project.CreatedBy, project.CreatedAt, project.UpdatedAt,
	)

	if err := row.Scan(&project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create project"})
		return
	}

	c.JSON(http.StatusCreated, project)
}

func GetProjects(c *gin.Context) {
	companyID := c.Query("company_id")

	var (
		query string
		args  []any
	)

	if companyID != "" {
		query = `SELECT id, company_id, name, description, status, created_by, created_at, updated_at
		          FROM projects WHERE company_id = $1 ORDER BY created_at DESC`
		args = []any{companyID}
	} else {
		query = `SELECT id, company_id, name, description, status, created_by, created_at, updated_at
		          FROM projects ORDER BY created_at DESC`
	}

	rows, err := config.DB.Query(context.Background(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch projects"})
		return
	}
	defer rows.Close()

	projects := []models.Project{}
	for rows.Next() {
		var p models.Project
		if err := rows.Scan(
			&p.ID, &p.CompanyID, &p.Name, &p.Description,
			&p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse projects"})
			return
		}
		projects = append(projects, p)
	}

	c.JSON(http.StatusOK, gin.H{"data": projects, "count": len(projects)})
}
