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
)

var validApptTypes = map[string]string{
	"present":     "นัดหมาย Present",
	"demo":        "นัดหมาย Demo",
	"site_survey": "นัดหมาย Site Survey",
}

type projectAppointment struct {
	Type            string `json:"type"`
	ScheduledAt     string `json:"scheduled_at"`
	Note            string `json:"note,omitempty"`
	PresentType     string `json:"present_type,omitempty"`
	CalendarEventID string `json:"calendar_event_id,omitempty"`
}

func GetProjectAppointments(c *gin.Context) {
	projectID := c.Param("id")
	rows, err := config.DB.Query(context.Background(),
		`SELECT appt_type, scheduled_at::text, COALESCE(note,''), COALESCE(present_type,''), COALESCE(calendar_event_id,'')
		 FROM project_appointments
		 WHERE project_id = $1::uuid
		 ORDER BY scheduled_at ASC`,
		projectID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load appointments"})
		return
	}
	defer rows.Close()

	list := []projectAppointment{}
	hasPresent := false
	for rows.Next() {
		var a projectAppointment
		if err := rows.Scan(&a.Type, &a.ScheduledAt, &a.Note, &a.PresentType, &a.CalendarEventID); err != nil {
			continue
		}
		if a.Type == "present" {
			hasPresent = true
		}
		list = append(list, a)
	}

	// Legacy projects may only have appointment_date on projects row (pre-migration).
	if !hasPresent {
		var legacyAt, legacyNote, legacyPresent string
		_ = config.DB.QueryRow(context.Background(),
			`SELECT COALESCE(appointment_date::text,''), COALESCE(appointment_note,''), COALESCE(present_type,'')
			 FROM projects WHERE id = $1::uuid`, projectID,
		).Scan(&legacyAt, &legacyNote, &legacyPresent)
		if strings.TrimSpace(legacyAt) != "" {
			list = append([]projectAppointment{{
				Type: "present", ScheduledAt: legacyAt, Note: legacyNote, PresentType: legacyPresent,
			}}, list...)
		}
	}

	c.JSON(http.StatusOK, list)
}

func UpsertProjectAppointment(c *gin.Context) {
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

	// Demo / Site Survey are optional add-ons — do not change pipeline status.
	if apptType == "demo" || apptType == "site_survey" {
		if projectStatus == "register" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณานัดหมาย Present ก่อน"})
			return
		}
	}

	var exists bool
	_ = config.DB.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM project_appointments WHERE project_id = $1::uuid AND appt_type = $2)`,
		projectID, apptType,
	).Scan(&exists)
	if exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "มีนัดหมายประเภทนี้แล้ว ไม่สามารถนัดซ้ำได้"})
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	calendarEventID, calendarOK, calendarMessage, calendarMailOK := scheduleAppointmentNotifications(
		c, projectID, label, agencyName, contactPerson, contactPhone, strings.TrimSpace(req.Note), startAt,
	)

	_, err = config.DB.Exec(context.Background(),
		`INSERT INTO project_appointments
			(project_id, appt_type, scheduled_at, note, present_type, calendar_event_id, created_by, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, NULLIF($5,''), NULLIF($6,''), NULLIF($7,'')::uuid, NOW())`,
		projectID, apptType, startAt, strings.TrimSpace(req.Note), req.PresentType, calendarEventID, userIDStr,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถบันทึกนัดหมายได้"})
		return
	}

	// Keep legacy columns in sync for the latest primary appointment (present only).
	if apptType == "present" {
		_, _ = config.DB.Exec(context.Background(),
			`UPDATE projects
			 SET appointment_date = $1, appointment_note = $2, present_type = $3,
			     calendar_event_id = NULLIF($4,''), last_activity_at = NOW(),
			     auto_reject_at = NOW() + INTERVAL '90 days'
			 WHERE id = $5::uuid`,
			startAt, strings.TrimSpace(req.Note), req.PresentType, calendarEventID, projectID,
		)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":               "บันทึกนัดหมายสำเร็จ",
		"type":                  apptType,
		"scheduled_at":          req.ScheduledAt,
		"calendar_event_id":     calendarEventID,
		"calendar_ok":           calendarOK || calendarMailOK,
		"calendar_message":      calendarMessage,
		"calendar_mail_ok":      calendarMailOK,
	})
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

func upsertProjectAppointmentRow(ctx context.Context, projectID, apptType, userID, presentType, calendarEventID string, startAt time.Time, note string) {
	_, _ = config.DB.Exec(ctx,
		`INSERT INTO project_appointments
			(project_id, appt_type, scheduled_at, note, present_type, calendar_event_id, created_by, updated_at)
		 VALUES ($1::uuid, $2, $3, $4, NULLIF($5,''), NULLIF($6,''), NULLIF($7,'')::uuid, NOW())
		 ON CONFLICT (project_id, appt_type)
		 DO UPDATE SET
		   scheduled_at = EXCLUDED.scheduled_at,
		   note = EXCLUDED.note,
		   present_type = EXCLUDED.present_type,
		   calendar_event_id = EXCLUDED.calendar_event_id,
		   updated_at = NOW()`,
		projectID, apptType, startAt, note, presentType, calendarEventID, userID,
	)
}
