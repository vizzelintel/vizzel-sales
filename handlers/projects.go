package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"vizzel-backend/config"
	"vizzel-backend/models"
)

// Rejects abbreviated agency names containing common short-forms or bare dots.
var abbrevPattern = regexp.MustCompile(`อบต\.?|อบจ\.?|ทต\.?|ทน\.?|ทม\.?|\.$|^\.|\.{2,}`)

// won and closing have been removed; auto-advance via document upload replaces manual doc gates.
var validStatuses = map[string]bool{
	"registrator": true,
	"present":     true,
	"demo":        true,
	"site_survey": true,
	"quotation":   true,
	"tor":         true,
	"contract":    true,
	"closed":      true,
	"reject":      true,
}

// Appointment statuses trigger a Google Calendar event when appointment_date is provided.
var appointmentStatusLabel = map[string]string{
	"present":     "นัดหมาย Present",
	"demo":        "นัดหมาย Demo",
	"site_survey": "นัดหมาย Site Survey",
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
	created_at,
	COALESCE(appointment_date::text,''),
	COALESCE(appointment_note,''),
	COALESCE(calendar_event_id,''),
	COALESCE(present_type,''),
	COALESCE(detail_note,'')`

func scanProject(row interface{ Scan(...any) error }) (models.Project, error) {
	var p models.Project
	err := row.Scan(
		&p.ID, &p.CompanyID, &p.AgencyName,
		&p.AgencyType, &p.Region,
		&p.ContactPerson, &p.ContactPosition, &p.ContactPhone,
		&p.Status, &p.StatusNote, &p.RejectReason,
		&p.CreatedBy, &p.CreatedAt,
		&p.AppointmentDate, &p.AppointmentNote, &p.CalendarEventID,
		&p.PresentType, &p.DetailNote,
	)
	return p, err
}

func CreateProject(c *gin.Context) {
	var req models.CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate agency name: must be non-empty, no abbreviations or bare dots.
	name := strings.TrimSpace(req.AgencyName)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณากรอกชื่อหน่วยงาน"})
		return
	}
	if abbrevPattern.MatchString(name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาใส่ชื่อหน่วยงานแบบเต็ม ไม่ใช้ตัวย่อหรือจุด (.) ในชื่อ"})
		return
	}
	req.AgencyName = name

	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	// Duplicate check: reject if agency_name already exists (case/space-insensitive).
	var existingID string
	dupErr := config.DB.QueryRow(context.Background(),
		`SELECT id::text FROM projects WHERE LOWER(TRIM(agency_name)) = LOWER(TRIM($1)) LIMIT 1`,
		name,
	).Scan(&existingID)
	if dupErr == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":               "หน่วยงานนี้ถูกลงทะเบียนไปแล้ว",
			"existing_project_id": existingID,
		})
		return
	}
	if !errors.Is(dupErr, pgx.ErrNoRows) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check duplicate"})
		return
	}

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
		&project.AppointmentDate, &project.AppointmentNote, &project.CalendarEventID,
		&project.PresentType, &project.DetailNote,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create project: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, project)
}

func GetProjects(c *gin.Context) {
	companyID := c.Query("company_id")
	search    := strings.TrimSpace(c.Query("search"))

	base := `SELECT ` + projectCols + ` FROM projects`
	var conditions []string
	var args []any

	if companyID != "" {
		args = append(args, companyID)
		conditions = append(conditions, fmt.Sprintf("company_id = $%d::uuid", len(args)))
	}
	if search != "" {
		args = append(args, "%"+strings.ToLower(search)+"%")
		conditions = append(conditions, fmt.Sprintf("LOWER(agency_name) LIKE $%d", len(args)))
	}

	query := base
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC"

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
	if req.Status == "reject" && strings.TrimSpace(req.RejectReason) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาระบุเหตุผลที่ปฏิเสธ"})
		return
	}
	// present requires present_type
	if req.Status == "present" && req.PresentType != "online" && req.PresentType != "onsite" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาระบุรูปแบบ Present (online หรือ onsite)"})
		return
	}

	// Fetch old status for logging
	var oldStatus string
	_ = config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(status,'') FROM projects WHERE id = $1::uuid`, id,
	).Scan(&oldStatus)

	// Google Calendar: fire for appointment statuses when appointment_date is provided.
	calendarEventID := ""
	calendarOK := false
	if statusLabel, isAppt := appointmentStatusLabel[req.Status]; isAppt && req.AppointmentDate != "" {
		var agencyName, contactPerson, contactPhone string
		_ = config.DB.QueryRow(context.Background(),
			`SELECT agency_name, COALESCE(contact_person,''), COALESCE(contact_phone,'')
			 FROM projects WHERE id = $1::uuid`, id,
		).Scan(&agencyName, &contactPerson, &contactPhone)

		var userEmail string
		lineID, _ := c.Get("line_id")
		if lineIDStr, _ := lineID.(string); lineIDStr != "" {
			_ = config.DB.QueryRow(context.Background(),
				`SELECT COALESCE(email,'') FROM users WHERE line_id = $1`, lineIDStr,
			).Scan(&userEmail)
		}
		attendees := []string{}
		if userEmail != "" {
			attendees = []string{userEmail}
		}
		title := fmt.Sprintf("[Vizzel] %s - %s", statusLabel, agencyName)
		desc := calendarDescription(contactPerson, contactPhone, req.AppointmentNote)
		if evID, err := CreateCalendarEvent(title, desc, req.AppointmentDate, attendees); err == nil {
			calendarEventID = evID
			calendarOK = true
		}
	}

	// auto_reject_at: NULL for terminal statuses; 90-day window otherwise
	autoRejectExpr := `NOW() + INTERVAL '90 days'`
	if req.Status == "contract" || req.Status == "closed" || req.Status == "reject" {
		autoRejectExpr = `NULL`
	}

	tag, err := config.DB.Exec(context.Background(),
		fmt.Sprintf(`UPDATE projects
		 SET status            = $1,
		     status_note       = NULLIF($2, ''),
		     reject_reason     = NULLIF($3, ''),
		     appointment_date  = NULLIF($4, '')::timestamptz,
		     appointment_note  = NULLIF($5, ''),
		     calendar_event_id = NULLIF($6, ''),
		     present_type      = NULLIF($7, ''),
		     last_activity_at  = NOW(),
		     auto_reject_at    = %s
		 WHERE id = $8::uuid`, autoRejectExpr),
		req.Status, req.StatusNote, req.RejectReason,
		req.AppointmentDate, req.AppointmentNote, calendarEventID,
		req.PresentType, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update status"})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	// Log status change
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)
	_, _ = config.DB.Exec(context.Background(),
		`INSERT INTO project_status_logs (project_id, from_status, to_status, changed_by, note)
		 VALUES ($1::uuid, $2, $3, NULLIF($4,'')::uuid, NULLIF($5,''))`,
		id, oldStatus, req.Status, userIDStr, req.StatusNote,
	)

	c.JSON(http.StatusOK, gin.H{
		"id":                id,
		"status":            req.Status,
		"present_type":      req.PresentType,
		"calendar_event_id": calendarEventID,
		"calendar_ok":       calendarOK,
	})
}

// UpdateProject handles PUT /api/v1/projects/:id for partial field updates
// (currently: detail_note only). Returns 403 if the project is closed.
func UpdateProject(c *gin.Context) {
	id := c.Param("id")

	var req models.UpdateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.DetailNote == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no fields to update"})
		return
	}

	note := *req.DetailNote
	if len([]rune(note)) > 2000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "รายละเอียดต้องไม่เกิน 2000 ตัวอักษร"})
		return
	}

	// Fetch current status to enforce closed-project lock
	var currentStatus string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(status,'') FROM projects WHERE id = $1::uuid`, id,
	).Scan(&currentStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch project"})
		}
		return
	}
	if currentStatus == "closed" {
		c.JSON(http.StatusForbidden, gin.H{"error": "ไม่สามารถแก้ไขได้ เนื่องจากงานปิดแล้ว"})
		return
	}

	tag, err := config.DB.Exec(context.Background(),
		`UPDATE projects SET detail_note = NULLIF($1,''), last_activity_at = NOW() WHERE id = $2::uuid`,
		note, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update project"})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": id, "detail_note": note})
}

func calendarDescription(contactPerson, contactPhone, note string) string {
	var parts []string
	if contactPerson != "" {
		parts = append(parts, "ผู้ติดต่อ: "+contactPerson)
	}
	if contactPhone != "" {
		parts = append(parts, "โทรศัพท์: "+contactPhone)
	}
	if note != "" {
		parts = append(parts, "\n"+note)
	}
	return strings.Join(parts, "\n")
}
