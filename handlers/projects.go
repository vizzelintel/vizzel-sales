package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"vizzel-backend/config"
	"vizzel-backend/models"
)

var validStatuses = map[string]bool{
	"registrator": true,
	"present":     true,
	"demo":        true,
	"site_survey": true,
	"quotation":   true,
	"tor":         true,
	"contract":    true,
	"won":         true,
	"closing":     true,
	"closed":      true,
	"reject":      true,
}

// Transitions to these statuses require >= 1 document attached to the project first.
var statusRequiresDocs = map[string]bool{
	"quotation": true,
	"contract":  true,
	"closing":   true,
}

const projectCols = `id,
	COALESCE(company_id::text,''),
	agency_name,
	COALESCE(agency_type,''),
	COALESCE(region,''),
	COALESCE(contact_person,''),
	COALESCE(contact_position,''),
	COALESCE(contact_phone,''),
	COALESCE(status,'registrator'),
	COALESCE(status_note,''),
	COALESCE(reject_reason,''),
	COALESCE(created_by::text,''),
	created_at`

func scanProject(row interface{ Scan(...any) error }) (models.Project, error) {
	var p models.Project
	err := row.Scan(
		&p.ID, &p.CompanyID, &p.AgencyName,
		&p.AgencyType, &p.Region,
		&p.ContactPerson, &p.ContactPosition, &p.ContactPhone,
		&p.Status, &p.StatusNote, &p.RejectReason,
		&p.CreatedBy, &p.CreatedAt,
	)
	return p, err
}

func CreateProject(c *gin.Context) {
	var req models.CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	now := time.Now().UTC()
	var project models.Project
	err := config.DB.QueryRow(context.Background(),
		`INSERT INTO projects
			(agency_name, agency_type, region,
			 contact_person, contact_position, contact_phone,
			 status, company_id, created_by, created_at)
		 VALUES ($1, NULLIF($2,''), $3, $4, $5, $6,
		         'registrator', NULLIF($7,'')::uuid, NULLIF($8,'')::uuid, $9)
		 RETURNING `+projectCols,
		req.AgencyName, req.AgencyType, req.Region,
		req.ContactPerson, req.ContactPosition, req.ContactPhone,
		req.CompanyID, userIDStr, now,
	).Scan(
		&project.ID, &project.CompanyID, &project.AgencyName,
		&project.AgencyType, &project.Region,
		&project.ContactPerson, &project.ContactPosition, &project.ContactPhone,
		&project.Status, &project.StatusNote, &project.RejectReason,
		&project.CreatedBy, &project.CreatedAt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create project: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, project)
}

func GetProjects(c *gin.Context) {
	companyID := c.Query("company_id")

	base := `SELECT ` + projectCols + ` FROM projects`
	var query string
	var args []any

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
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse project"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}

	// reject requires a reason
	if req.Status == "reject" && strings.TrimSpace(req.RejectReason) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาระบุเหตุผลที่ปฏิเสธ"})
		return
	}

	// doc-gated statuses: project must have >= 1 document attached
	if statusRequiresDocs[req.Status] {
		var count int
		if err := config.DB.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM documents WHERE project_id = $1::uuid`, id,
		).Scan(&count); err != nil || count == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาแนบเอกสารก่อนเปลี่ยนสถานะ"})
			return
		}
	}

	tag, err := config.DB.Exec(context.Background(),
		`UPDATE projects
		 SET status        = $1,
		     status_note   = NULLIF($2, ''),
		     reject_reason = NULLIF($3, '')
		 WHERE id = $4::uuid`,
		req.Status, req.StatusNote, req.RejectReason, id,
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
