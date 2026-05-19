package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"vizzel-backend/config"
	"vizzel-backend/models"
)

var validStatuses = map[string]bool{
	"prospect": true, "trial": true, "negotiation": true, "won": true, "lost": true,
}

func CreateProject(c *gin.Context) {
	var req models.CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	lineUserID := c.GetString("line_id")

	status := req.Status
	if status == "" || !validStatuses[status] {
		status = "prospect"
	}

	now := time.Now().UTC()
	project := models.Project{
		CompanyID:     req.CompanyID,
		AgencyName:    req.AgencyName,
		Region:        req.Region,
		ContactPerson: req.ContactPerson,
		ContactPhone:  req.ContactPhone,
		Status:        status,
		CreatedBy:     lineUserID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	err := config.DB.QueryRow(context.Background(),
		`INSERT INTO projects (company_id, agency_name, region, contact_person, contact_phone, status, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id`,
		project.CompanyID, project.AgencyName, project.Region,
		project.ContactPerson, project.ContactPhone, project.Status,
		project.CreatedBy, project.CreatedAt, project.UpdatedAt,
	).Scan(&project.ID)
	if err != nil {
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
		query = `SELECT id, company_id, agency_name, region, contact_person, contact_phone, status, created_by, created_at, updated_at
		          FROM projects WHERE company_id = $1 ORDER BY created_at DESC`
		args = []any{companyID}
	} else {
		query = `SELECT id, company_id, agency_name, region, contact_person, contact_phone, status, created_by, created_at, updated_at
		          FROM projects ORDER BY created_at DESC`
	}

	rows, err := config.DB.Query(context.Background(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch projects"})
		return
	}
	defer rows.Close()

	projects := make([]models.Project, 0)
	for rows.Next() {
		var p models.Project
		if err := rows.Scan(
			&p.ID, &p.CompanyID, &p.AgencyName, &p.Region,
			&p.ContactPerson, &p.ContactPhone, &p.Status,
			&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse projects"})
			return
		}
		projects = append(projects, p)
	}

	c.JSON(http.StatusOK, projects)
}

func GetProject(c *gin.Context) {
	id := c.Param("id")

	var p models.Project
	err := config.DB.QueryRow(context.Background(),
		`SELECT id, company_id, agency_name, region, contact_person, contact_phone, status, created_by, created_at, updated_at
		 FROM projects WHERE id = $1`, id,
	).Scan(
		&p.ID, &p.CompanyID, &p.AgencyName, &p.Region,
		&p.ContactPerson, &p.ContactPhone, &p.Status,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch project"})
		}
		return
	}

	c.JSON(http.StatusOK, p)
}

func UpdateProjectStatus(c *gin.Context) {
	id := c.Param("id")

	var req models.UpdateProjectStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !validStatuses[req.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status, must be one of: prospect, trial, negotiation, won, lost"})
		return
	}

	now := time.Now().UTC()
	tag, err := config.DB.Exec(context.Background(),
		`UPDATE projects SET status = $1, updated_at = $2 WHERE id = $3`,
		req.Status, now, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update status"})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": id, "status": req.Status, "updated_at": now})
}
