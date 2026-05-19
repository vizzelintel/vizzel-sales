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

const projectCols = `id, COALESCE(company_id::text,''), agency_name,
	COALESCE(region,''), COALESCE(contact_person,''), COALESCE(contact_phone,''),
	COALESCE(status,'prospect'), COALESCE(created_by::text,''), created_at`

func scanProject(row interface{ Scan(...any) error }) (models.Project, error) {
	var p models.Project
	err := row.Scan(
		&p.ID, &p.CompanyID, &p.AgencyName,
		&p.Region, &p.ContactPerson, &p.ContactPhone,
		&p.Status, &p.CreatedBy, &p.CreatedAt,
	)
	return p, err
}

func CreateProject(c *gin.Context) {
	var req models.CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	status := req.Status
	if status == "" || !validStatuses[status] {
		status = "prospect"
	}

	// user_id is the DB UUID set by JWTAuth middleware from the "uid" JWT claim.
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	now := time.Now().UTC()

	var project models.Project
	err := config.DB.QueryRow(context.Background(),
		`INSERT INTO projects (agency_name, region, contact_person, contact_phone, status, company_id, created_by, created_at)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6,'')::uuid, NULLIF($7,'')::uuid, $8)
		 RETURNING `+projectCols,
		req.AgencyName, req.Region, req.ContactPerson, req.ContactPhone,
		status, req.CompanyID, userIDStr, now,
	).Scan(
		&project.ID, &project.CompanyID, &project.AgencyName,
		&project.Region, &project.ContactPerson, &project.ContactPhone,
		&project.Status, &project.CreatedBy, &project.CreatedAt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create project: " + err.Error()})
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

	base := `SELECT ` + projectCols + ` FROM projects`
	if companyID != "" {
		query = base + ` WHERE company_id = $1::uuid ORDER BY created_at DESC`
		args = []any{companyID}
	} else {
		query = base + ` ORDER BY created_at DESC`
	}

	rows, err := config.DB.Query(context.Background(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch projects"})
		return
	}
	defer rows.Close()

	projects := make([]models.Project, 0)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse projects"})
			return
		}
		projects = append(projects, p)
	}

	c.JSON(http.StatusOK, projects)
}

func GetProject(c *gin.Context) {
	id := c.Param("id")

	p, err := scanProject(config.DB.QueryRow(context.Background(),
		`SELECT `+projectCols+` FROM projects WHERE id = $1::uuid`, id,
	))
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

	tag, err := config.DB.Exec(context.Background(),
		`UPDATE projects SET status = $1 WHERE id = $2::uuid`,
		req.Status, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update status"})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": id, "status": req.Status})
}
