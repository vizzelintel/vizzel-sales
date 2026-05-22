package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"vizzel-backend/config"
)

const maxAppointmentsPerType = 10

var validApptTypes = map[string]string{
	"present":     "นัดหมาย Present",
	"demo":        "นัดหมาย Demo",
	"site_survey": "นัดหมาย Site Survey",
}

type projectAppointment struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	ScheduledAt     string `json:"scheduled_at"`
	Note            string `json:"note,omitempty"`
	PresentType     string `json:"present_type,omitempty"`
	CalendarEventID string `json:"calendar_event_id,omitempty"`
}

func GetProjectAppointments(c *gin.Context) {
	projectID := c.Param("id")

	list, err := listProjectAppointments(context.Background(), projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load appointments"})
		return
	}
	c.JSON(http.StatusOK, list)
}

func listProjectAppointments(ctx context.Context, projectID string) ([]projectAppointment, error) {
	rows, err := config.DB.Query(ctx,
		`SELECT id::text, appt_type, scheduled_at::text, COALESCE(note,''), COALESCE(present_type,''), COALESCE(calendar_event_id,'')
		 FROM project_appointments
		 WHERE project_id = $1::uuid
		 ORDER BY appt_type, scheduled_at ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []projectAppointment{}
	hasPresent := false
	for rows.Next() {
		var a projectAppointment
		if err := rows.Scan(&a.ID, &a.Type, &a.ScheduledAt, &a.Note, &a.PresentType, &a.CalendarEventID); err != nil {
			continue
		}
		if a.Type == "present" {
			hasPresent = true
		}
		list = append(list, a)
	}

	if !hasPresent {
		var legacyAt, legacyNote, legacyPresent string
		_ = config.DB.QueryRow(ctx,
			`SELECT COALESCE(appointment_date::text,''), COALESCE(appointment_note,''), COALESCE(present_type,'')
			 FROM projects WHERE id = $1::uuid`, projectID,
		).Scan(&legacyAt, &legacyNote, &legacyPresent)
		if strings.TrimSpace(legacyAt) != "" {
			list = append([]projectAppointment{{
				ID:          "",
				Type:        "present",
				ScheduledAt: legacyAt,
				Note:        legacyNote,
				PresentType: legacyPresent,
			}}, list...)
		}
	}
	return list, nil
}

func countAppointmentsByType(ctx context.Context, projectID, apptType string) (int, error) {
	var n int
	err := config.DB.QueryRow(ctx,
		`SELECT COUNT(*) FROM project_appointments WHERE project_id = $1::uuid AND appt_type = $2`,
		projectID, apptType,
	).Scan(&n)
	return n, err
}

// CreateProjectAppointment adds one appointment row (max 10 per type). Does not change pipeline status.
func CreateProjectAppointment(c *gin.Context) {
	projectID := c.Param("id")
	apptType := strings.TrimSpace(c.Param("type"))
	label, ok := validApptTypes[apptType]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ประเภทนัดหมายไม่ถูกต้อง"})
		return
	}

	var req struct {
		ScheduledAt string `json:"scheduled_at" binding:"required"`
		Note        string `json:"note"`
		PresentType string `json:"present_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if apptType == "present" && req.PresentType != "online" && req.PresentType != "onsite" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณาระบุรูปแบบ Present (online หรือ onsite)"})
		return
	}

	startAt, err := time.Parse(time.RFC3339, req.ScheduledAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "รูปแบบวันเวลานัดหมายไม่ถูกต้อง"})
		return
	}

	var projectStatus, agencyName, contactPerson, contactPhone string
	err = config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(status,''), agency_name, COALESCE(contact_person,''), COALESCE(contact_phone,'')
		 FROM projects WHERE id = $1::uuid`, projectID,
	).Scan(&projectStatus, &agencyName, &contactPerson, &contactPhone)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		}
		return
	}
	if projectStatus == "closed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ไม่สามารถนัดหมายได้ เนื่องจากงานปิดแล้ว"})
		return
	}

	ctx := context.Background()
	n, err := countAppointmentsByType(ctx, projectID, apptType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	if n >= maxAppointmentsPerType {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("นัด%sได้สูงสุด %d ครั้ง", label, maxAppointmentsPerType)})
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	calendarEventID, calendarOK, calendarMessage, calendarMailOK := scheduleAppointmentNotifications(
		c, projectID, label, agencyName, contactPerson, contactPhone, strings.TrimSpace(req.Note), startAt,
	)

	var apptID string
	err = config.DB.QueryRow(ctx,
		`INSERT INTO project_appointments
			(project_id, appt_type, scheduled_at, note, present_type, calendar_event_id, created_by, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, NULLIF($5,''), NULLIF($6,''), NULLIF($7,'')::uuid, NOW())
		 RETURNING id::text`,
		projectID, apptType, startAt, strings.TrimSpace(req.Note), req.PresentType, calendarEventID, userIDStr,
	).Scan(&apptID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถบันทึกนัดหมายได้"})
		return
	}

	syncLatestPresentLegacyColumns(ctx, projectID)

	c.JSON(http.StatusCreated, gin.H{
		"message":               "บันทึกนัดหมายสำเร็จ",
		"id":                    apptID,
		"type":                  apptType,
		"scheduled_at":          req.ScheduledAt,
		"calendar_event_id":     calendarEventID,
		"calendar_ok":           calendarOK || calendarMailOK,
		"calendar_message":      calendarMessage,
		"calendar_mail_ok":      calendarMailOK,
	})
	go SyncProjectToLarkByID(projectID)
}

// UpsertProjectAppointment is kept for backward compatibility — creates a new row (same as POST).
func UpsertProjectAppointment(c *gin.Context) {
	CreateProjectAppointment(c)
}

// DeleteProjectAppointment removes one appointment by id.
func DeleteProjectAppointment(c *gin.Context) {
	projectID := c.Param("id")
	apptID := strings.TrimSpace(c.Param("apptId"))
	if apptID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid appointment id"})
		return
	}

	var projectStatus string
	err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(status,'') FROM projects WHERE id = $1::uuid`, projectID,
	).Scan(&projectStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		}
		return
	}
	if projectStatus == "closed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ไม่สามารถลบนัดหมายได้ เนื่องจากงานปิดแล้ว"})
		return
	}

	tag, err := config.DB.Exec(context.Background(),
		`DELETE FROM project_appointments WHERE id = $1::uuid AND project_id = $2::uuid`,
		apptID, projectID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ลบนัดหมายไม่สำเร็จ"})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบนัดหมาย"})
		return
	}

	syncLatestPresentLegacyColumns(context.Background(), projectID)
	c.JSON(http.StatusOK, gin.H{"message": "ลบนัดหมายสำเร็จ"})
	go SyncProjectToLarkByID(projectID)
}

// syncLatestPresentLegacyColumns keeps projects.appointment_* in sync with the latest present row.
func syncLatestPresentLegacyColumns(ctx context.Context, projectID string) {
	var at, note, pt, calID string
	err := config.DB.QueryRow(ctx,
		`SELECT scheduled_at::text, COALESCE(note,''), COALESCE(present_type,''), COALESCE(calendar_event_id,'')
		 FROM project_appointments
		 WHERE project_id = $1::uuid AND appt_type = 'present'
		 ORDER BY scheduled_at DESC
		 LIMIT 1`, projectID,
	).Scan(&at, &note, &pt, &calID)
	if err != nil {
		_, _ = config.DB.Exec(ctx,
			`UPDATE projects
			 SET appointment_date = NULL, appointment_note = NULL, present_type = NULL,
			     calendar_event_id = NULL, last_activity_at = NOW()
			 WHERE id = $1::uuid`, projectID,
		)
		return
	}
	_, _ = config.DB.Exec(ctx,
		`UPDATE projects
		 SET appointment_date = $1::timestamptz, appointment_note = $2, present_type = NULLIF($3,''),
		     calendar_event_id = NULLIF($4,''), last_activity_at = NOW(),
		     auto_reject_at = COALESCE(auto_reject_at, NOW() + INTERVAL '90 days')
		 WHERE id = $5::uuid`,
		at, note, pt, calID, projectID,
	)
}

// scheduleAppointmentNotifications tries Google Calendar then SMTP .ics invite.
func scheduleAppointmentNotifications(
	c *gin.Context,
	projectID, statusLabel, agencyName, contactPerson, contactPhone, note string,
	startAt time.Time,
) (calendarEventID string, calendarOK bool, calendarMessage string, calendarMailOK bool) {
	desc := calendarDescription(contactPerson, contactPhone, note)
	title := "[Vizzel] " + statusLabel + " - " + agencyName
	dt := startAt.UTC().Format(time.RFC3339)

	if evID, err := CreateCalendarEvent(title, desc, dt, attendeeEmails(c)); err == nil {
		calendarEventID = evID
		calendarOK = true
		calendarMessage = "บันทึกนัดหมายใน Google Calendar แล้ว"
	} else if !isGoogleCalendarSkipped(err) {
		calendarMessage = "ไม่สามารถบันทึก Google Calendar ได้: " + err.Error()
	}

	var userEmail string
	lineID, _ := c.Get("line_id")
	if lineIDStr, _ := lineID.(string); lineIDStr != "" {
		_ = config.DB.QueryRow(context.Background(),
			`SELECT COALESCE(email,'') FROM users WHERE line_id = $1`, lineIDStr,
		).Scan(&userEmail)
	}
	if userEmail == "" {
		if calendarMessage == "" {
			calendarMessage = "ไม่พบอีเมลผู้ใช้งานสำหรับส่งคำเชิญปฏิทิน"
		}
		return calendarEventID, calendarOK, calendarMessage, false
	}
	if err := SendCalendarInviteEmail(userEmail, agencyName, statusLabel, note, startAt, projectID); err == nil {
		calendarMailOK = true
		if calendarMessage == "" {
			calendarMessage = "ส่งคำเชิญปฏิทินทางอีเมลแล้ว"
		}
	} else if calendarMessage == "" {
		calendarMessage = "ส่งคำเชิญปฏิทินทางอีเมลไม่สำเร็จ: " + err.Error()
	}
	return calendarEventID, calendarOK, calendarMessage, calendarMailOK
}

func attendeeEmails(c *gin.Context) []string {
	lineID, _ := c.Get("line_id")
	lineIDStr, _ := lineID.(string)
	if lineIDStr == "" {
		return nil
	}
	var userEmail string
	_ = config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(email,'') FROM users WHERE line_id = $1`, lineIDStr,
	).Scan(&userEmail)
	if userEmail == "" {
		return nil
	}
	return []string{userEmail}
}

func isGoogleCalendarSkipped(err error) bool {
	if err == nil {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "not configured") ||
		strings.Contains(msg, "invalid") ||
		strings.Contains(msg, "parse credentials")
}

func insertProjectAppointmentRow(ctx context.Context, projectID, apptType, userID, presentType, calendarEventID string, startAt time.Time, note string) error {
	_, err := config.DB.Exec(ctx,
		`INSERT INTO project_appointments
			(project_id, appt_type, scheduled_at, note, present_type, calendar_event_id, created_by, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, NULLIF($5,''), NULLIF($6,''), NULLIF($7,'')::uuid, NOW())`,
		projectID, apptType, startAt, note, presentType, calendarEventID, userID,
	)
	if err == nil && apptType == "present" {
		syncLatestPresentLegacyColumns(ctx, projectID)
	}
	return err
}
