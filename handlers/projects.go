package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
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
	"register":  true,
	"present":   true,
	"quotation": true,
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
	COALESCE(status,'register'),
	COALESCE(status_note,''),
	COALESCE(reject_reason,''),
	COALESCE(created_by::text,''),
	created_at,
	COALESCE(appointment_date::text,''),
	COALESCE(appointment_note,''),
	COALESCE(calendar_event_id,''),
	COALESCE(present_type,''),
	COALESCE(detail_note,''),
	COALESCE(auto_reject_at::text,''),
	COALESCE(lark_record_id,'')`

func scanProject(row interface{ Scan(...any) error }) (models.Project, error) {
	var p models.Project
	err := row.Scan(
		&p.ID, &p.CompanyID, &p.AgencyName,
		&p.AgencyType, &p.Region,
		&p.ContactPerson, &p.ContactPosition, &p.ContactPhone,
		&p.Status, &p.StatusNote, &p.RejectReason,
		&p.CreatedBy, &p.CreatedAt,
		&p.AppointmentDate, &p.AppointmentNote, &p.CalendarEventID,
		&p.PresentType, &p.DetailNote, &p.AutoRejectAt,
		&p.LarkRecordID,
	)
	// Backward compatibility: older DB constraints may still store "registrator".
	// Keep API contract stable by normalizing to "register" for clients.
	if p.Status == "registrator" {
		p.Status = "register"
	}
	// Present is no longer a pipeline stage; treat legacy rows as register in API responses.
	if p.Status == "present" {
		p.Status = "register"
	}
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

	// Auto-set company_id from the creator's company if not provided
	if req.CompanyID == "" {
		var creatorCompanyID string
		_ = config.DB.QueryRow(context.Background(),
			`SELECT COALESCE(company_id::text,'') FROM users WHERE id = $1::uuid`, userIDStr,
		).Scan(&creatorCompanyID)
		req.CompanyID = creatorCompanyID
	}

	// Duplicate check: same agency_name allowed only when every existing row is reject.
	var existingID, existingStatus string
	dupErr := config.DB.QueryRow(context.Background(),
		`SELECT id::text, COALESCE(status,'')
		 FROM projects
		 WHERE LOWER(TRIM(agency_name)) = LOWER(TRIM($1))
		   AND status IS DISTINCT FROM 'reject'
		 LIMIT 1`,
		name,
	).Scan(&existingID, &existingStatus)
	if dupErr == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "มีโครงการชื่อนี้อยู่แล้ว (สถานะ: " + getLarkStatusLabel(existingStatus) +
				") สร้างใหม่ได้เมื่อโครงการเดิมเป็น Reject เท่านั้น",
			"existing_project_id":     existingID,
			"existing_project_status": existingStatus,
		})
		return
	}
	if !errors.Is(dupErr, pgx.ErrNoRows) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check duplicate"})
		return
	}

	now := time.Now().UTC()
	var project models.Project
	var err error
	insertSQL := `INSERT INTO projects
			(agency_name, agency_type, region,
			 contact_person, contact_position, contact_phone,
			 status, company_id, created_by, created_at,
			 auto_reject_at)
		 VALUES ($1, NULLIF($2,''), $3, $4, $5, $6,
		         $10, NULLIF($7,'')::uuid, NULLIF($8,'')::uuid, $9,
		         NOW() + INTERVAL '90 days')
		 RETURNING ` + projectCols

	// Try "register" first (new canonical value), then fallback to legacy
	// "registrator" for databases that still have old check constraints.
	for _, dbStatus := range []string{"register", "registrator"} {
		err = config.DB.QueryRow(context.Background(),
			insertSQL,
			req.AgencyName, req.AgencyType, req.Region,
			req.ContactPerson, req.ContactPosition, req.ContactPhone,
			req.CompanyID, userIDStr, now, dbStatus,
		).Scan(
			&project.ID, &project.CompanyID, &project.AgencyName,
			&project.AgencyType, &project.Region,
			&project.ContactPerson, &project.ContactPosition, &project.ContactPhone,
			&project.Status, &project.StatusNote, &project.RejectReason,
			&project.CreatedBy, &project.CreatedAt,
			&project.AppointmentDate, &project.AppointmentNote, &project.CalendarEventID,
			&project.PresentType, &project.DetailNote, &project.AutoRejectAt,
			&project.LarkRecordID,
		)
		if err == nil {
			break
		}
		// Fallback only for legacy status check constraint mismatch.
		if !strings.Contains(err.Error(), "projects_status_check") {
			break
		}
	}
	if err != nil {
		if strings.Contains(err.Error(), "23505") &&
			strings.Contains(err.Error(), "idx_projects_agency_name_active") {
			c.JSON(http.StatusConflict, gin.H{
				"error": "มีโครงการชื่อนี้อยู่แล้ว สร้างใหม่ได้เมื่อโครงการเดิมเป็น Reject เท่านั้น",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create project: " + err.Error()})
		return
	}
	if project.Status == "registrator" {
		project.Status = "register"
	}

	c.JSON(http.StatusCreated, project)
	go SyncProjectToLark(project)
}

func normalizeProjectsScope(scope string) string {
	if strings.TrimSpace(scope) == "pipeline" {
		return "pipeline"
	}
	return "directory"
}

func GetProjects(c *gin.Context) {
	search := strings.TrimSpace(c.Query("search"))
	company := strings.TrimSpace(c.Query("company")) // company name filter
	agencyType := strings.TrimSpace(c.Query("agency_type"))
	province := strings.TrimSpace(c.Query("province")) // maps to projects.region
	statusF := strings.TrimSpace(c.Query("status"))
	scope := normalizeProjectsScope(c.DefaultQuery("scope", "directory"))

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)
	var callerRole, callerCompanyID string
	config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,''), COALESCE(company_id::text,'') FROM users WHERE id = $1::uuid`, uid,
	).Scan(&callerRole, &callerCompanyID)

	var conditions []string
	var args []any

	// Dealer sees only own company on Pipeline; Projects directory shows all companies.
	if scope == "pipeline" && callerRole == "dealer" && callerCompanyID != "" {
		args = append(args, callerCompanyID)
		conditions = append(conditions, fmt.Sprintf("company_id = $%d::uuid", len(args)))
	}
	if search != "" {
		args = append(args, "%"+strings.ToLower(search)+"%")
		conditions = append(conditions, fmt.Sprintf("LOWER(agency_name) LIKE $%d", len(args)))
	}
	if company != "" && callerRole != "dealer" {
		args = append(args, company)
		conditions = append(conditions, fmt.Sprintf(
			`company_id = (SELECT id FROM companies WHERE LOWER(name) = LOWER($%d) LIMIT 1)`, len(args)))
	}
	if agencyType != "" {
		args = append(args, agencyType)
		conditions = append(conditions, fmt.Sprintf("agency_type = $%d", len(args)))
	}
	if province != "" {
		args = append(args, province)
		conditions = append(conditions, fmt.Sprintf("region = $%d", len(args)))
	}
	listConditions := append([]string{}, conditions...)
	listArgs := append([]any{}, args...)
	if statusF != "" {
		listArgs = append(listArgs, statusF)
		listConditions = append(listConditions, fmt.Sprintf("status = $%d", len(listArgs)))
	}

	whereBase := ""
	if len(conditions) > 0 {
		whereBase = " WHERE " + strings.Join(conditions, " AND ")
	}
	whereList := whereBase
	if len(listConditions) > len(conditions) {
		whereList = " WHERE " + strings.Join(listConditions, " AND ")
	}

	ctx := context.Background()
	var total int
	if err := config.DB.QueryRow(ctx, `SELECT COUNT(*) FROM projects`+whereList, listArgs...).Scan(&total); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to count projects"})
		return
	}

	statusCounts := map[string]int{}
	countRows, err := config.DB.Query(ctx,
		`SELECT COALESCE(status,''), COUNT(*)::int FROM projects`+whereBase+` GROUP BY status`, args...)
	if err == nil {
		defer countRows.Close()
		for countRows.Next() {
			var st string
			var n int
			if countRows.Scan(&st, &n) == nil {
				if st == "present" {
					statusCounts["register"] += n
				} else {
					statusCounts[st] = n
				}
			}
		}
	}

	orderBy := ` ORDER BY created_at DESC`
	if scope == "pipeline" {
		orderBy = ` ORDER BY COALESCE(last_activity_at, created_at) DESC`
	}

	offset := (page - 1) * limit
	pageArgs := append(append([]any{}, listArgs...), limit, offset)
	query := `SELECT ` + projectCols + ` FROM projects` + whereList + orderBy +
		fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(listArgs)+1, len(listArgs)+2)

	rows, err := config.DB.Query(ctx, query, pageArgs...)
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

	totalPages := int(math.Ceil(float64(total) / float64(limit)))
	if totalPages < 1 {
		totalPages = 1
	}

	c.JSON(http.StatusOK, gin.H{
		"items":          projects,
		"total":          total,
		"page":           page,
		"limit":          limit,
		"total_pages":    totalPages,
		"status_counts":  statusCounts,
	})
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

	if strings.TrimSpace(p.LarkRecordID) != "" {
		if pullErr := MaybePullProjectFromLarkOnView(id, p.LarkRecordID); pullErr != nil {
			log.Printf("[LARK] pull on view project=%s: %v\n", id, pullErr)
		} else if p2, err2 := scanProject(config.DB.QueryRow(context.Background(),
			`SELECT `+projectCols+` FROM projects WHERE id = $1::uuid`, id,
		)); err2 == nil {
			p = p2
		}
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
	if oldStatus == "closed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ไม่สามารถเปลี่ยนสถานะได้ เนื่องจากงานปิดแล้ว"})
		return
	}

	// Google Calendar: fire for appointment statuses when appointment_date is provided.
	calendarEventID := ""
	calendarOK := false
	calendarMessage := ""
	calendarMailOK := false
	calendarMailMessage := ""
	if statusLabel, isAppt := appointmentStatusLabel[req.Status]; isAppt && req.AppointmentDate != "" {
		var agencyName, contactPerson, contactPhone string
		_ = config.DB.QueryRow(context.Background(),
			`SELECT agency_name, COALESCE(contact_person,''), COALESCE(contact_phone,'')
			 FROM projects WHERE id = $1::uuid`, id,
		).Scan(&agencyName, &contactPerson, &contactPhone)

		startAt, parseErr := time.Parse(time.RFC3339, req.AppointmentDate)
		if parseErr != nil {
			calendarMessage = "รูปแบบวันเวลานัดหมายไม่ถูกต้อง"
		} else {
			calendarEventID, calendarOK, calendarMessage, calendarMailOK = scheduleAppointmentNotifications(
				c, id, statusLabel, agencyName, contactPerson, contactPhone, req.AppointmentNote, startAt,
			)
			if !calendarOK && !calendarMailOK && calendarMessage == "" {
				calendarMessage = "ไม่สามารถบันทึกปฏิทินได้"
			}
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

	if req.AppointmentDate != "" {
		if _, isAppt := appointmentStatusLabel[req.Status]; isAppt {
			if startAt, err := time.Parse(time.RFC3339, req.AppointmentDate); err == nil {
				_ = insertProjectAppointmentRow(context.Background(), id, req.Status, userIDStr, req.PresentType, "", "", calendarEventID, startAt, req.AppointmentNote)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"id":                    id,
		"status":                req.Status,
		"present_type":          req.PresentType,
		"calendar_event_id":     calendarEventID,
		"calendar_ok":           calendarOK || calendarMailOK,
		"calendar_message":      calendarMessage,
		"calendar_mail_ok":      calendarMailOK,
		"calendar_mail_message": calendarMailMessage,
	})
	// Sync updated project to Lark in background
	go func() {
		if p, err := scanProject(config.DB.QueryRow(context.Background(),
			`SELECT `+projectCols+` FROM projects WHERE id = $1::uuid`, id,
		)); err == nil {
			SyncProjectToLark(p)
		}
	}()
}

// UpdateProject handles PUT /api/v1/projects/:id for partial field updates.
// Returns 403 if the project is closed.
func UpdateProject(c *gin.Context) {
	id := c.Param("id")

	var req models.UpdateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)

	var currentStatus, companyID string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(status,''), COALESCE(company_id::text,'') FROM projects WHERE id = $1::uuid`, id,
	).Scan(&currentStatus, &companyID); err != nil {
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

	var callerRole, callerCompanyID string
	_ = config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,''), COALESCE(company_id::text,'') FROM users WHERE id = $1::uuid`, uid,
	).Scan(&callerRole, &callerCompanyID)
	if callerRole == "dealer" && callerCompanyID != "" && companyID != callerCompanyID {
		c.JSON(http.StatusForbidden, gin.H{"error": "ไม่มีสิทธิ์แก้ไขโครงการนี้"})
		return
	}

	sets := []string{"last_activity_at = NOW()"}
	args := []any{}
	n := 0
	addSet := func(col string, val interface{}) {
		n++
		sets = append(sets, fmt.Sprintf("%s = $%d", col, n))
		args = append(args, val)
	}

	if req.AgencyName != nil {
		name := strings.TrimSpace(*req.AgencyName)
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณากรอกชื่อหน่วยงาน"})
			return
		}
		if abbrevPattern.MatchString(name) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาใส่ชื่อหน่วยงานแบบเต็ม ไม่ใช้ตัวย่อหรือจุด (.) ในชื่อ"})
			return
		}
		var existingID string
		dupErr := config.DB.QueryRow(context.Background(),
			`SELECT id::text FROM projects
			 WHERE LOWER(TRIM(agency_name)) = LOWER(TRIM($1))
			   AND status IS DISTINCT FROM 'reject'
			   AND id <> $2::uuid
			 LIMIT 1`,
			name, id,
		).Scan(&existingID)
		if dupErr == nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":               "มีโครงการชื่อนี้อยู่แล้ว สร้างใหม่ได้เมื่อโครงการเดิมเป็น Reject เท่านั้น",
				"existing_project_id": existingID,
			})
			return
		}
		if !errors.Is(dupErr, pgx.ErrNoRows) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check duplicate"})
			return
		}
		addSet("agency_name", name)
	}
	if req.AgencyType != nil {
		addSet("agency_type", strings.TrimSpace(*req.AgencyType))
	}
	if req.Region != nil {
		addSet("region", strings.TrimSpace(*req.Region))
	}
	if req.ContactPerson != nil {
		addSet("contact_person", strings.TrimSpace(*req.ContactPerson))
	}
	if req.ContactPosition != nil {
		addSet("contact_position", strings.TrimSpace(*req.ContactPosition))
	}
	if req.ContactPhone != nil {
		addSet("contact_phone", strings.TrimSpace(*req.ContactPhone))
	}
	if req.DetailNote != nil {
		note := *req.DetailNote
		if len([]rune(note)) > 2000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "รายละเอียดต้องไม่เกิน 2000 ตัวอักษร"})
			return
		}
		addSet("detail_note", note)
	}

	if n == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no fields to update"})
		return
	}

	if currentStatus != "contract" && currentStatus != "closed" && currentStatus != "reject" {
		sets = append(sets, "auto_reject_at = NOW() + INTERVAL '90 days'")
	}

	n++
	args = append(args, id)
	query := fmt.Sprintf(`UPDATE projects SET %s WHERE id = $%d::uuid`, strings.Join(sets, ", "), n)
	tag, err := config.DB.Exec(context.Background(), query, args...)
	if err != nil {
		if strings.Contains(err.Error(), "23505") &&
			strings.Contains(err.Error(), "idx_projects_agency_name_active") {
			c.JSON(http.StatusConflict, gin.H{
				"error": "มีโครงการชื่อนี้อยู่แล้ว สร้างใหม่ได้เมื่อโครงการเดิมเป็น Reject เท่านั้น",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update project"})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	p, err := scanProject(config.DB.QueryRow(context.Background(),
		`SELECT `+projectCols+` FROM projects WHERE id = $1::uuid`, id,
	))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch updated project"})
		return
	}

	c.JSON(http.StatusOK, p)
	go SyncProjectToLark(p)
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
