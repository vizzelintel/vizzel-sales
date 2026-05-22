package handlers

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"vizzel-backend/config"
)

func larkFieldIsEmpty(v interface{}) bool {
	if v == nil {
		return true
	}
	s := strings.TrimSpace(larkFieldText(v))
	return s == "" || s == "—"
}

func larkFieldByNames(fields map[string]interface{}, names ...string) (interface{}, bool) {
	for _, n := range names {
		if v, ok := fields[n]; ok {
			return v, true
		}
	}
	return nil, false
}

func parseLarkBitableDate(v interface{}) (time.Time, bool) {
	if larkFieldIsEmpty(v) {
		return time.Time{}, false
	}
	switch x := v.(type) {
	case float64:
		ms := int64(x)
		if ms > 1e12 {
			return time.UnixMilli(ms), true
		}
		if ms > 1e9 {
			return time.Unix(ms, 0), true
		}
	case int64:
		if x > 1e12 {
			return time.UnixMilli(x), true
		}
		return time.Unix(x, 0), true
	case int:
		return parseLarkBitableDate(int64(x))
	case string:
		return parseLarkTimestamp(x)
	case map[string]interface{}:
		if arr, ok := x["value"].([]interface{}); ok && len(arr) > 0 {
			return parseLarkBitableDate(arr[0])
		}
		if t := larkFieldText(x); t != "" {
			return parseLarkTimestamp(t)
		}
	}
	return time.Time{}, false
}

type larkInboundSlot struct {
	HasTime     bool
	ScheduledAt time.Time
	Note        string
	PresentType string
	MeetLink    string
	touched     bool
}

func parsePresentTypeField(raw interface{}) (string, bool) {
	if raw == nil {
		return "", false
	}
	pt := strings.TrimSpace(larkFieldText(raw))
	if pt == "" || pt == "—" {
		return "", true
	}
	if pt == "online" || pt == "onsite" {
		return pt, true
	}
	return "", false
}

func parseInboundSlot(fields map[string]interface{}, n int, dateCols, meetCols, noteCols, typeCols []string) larkInboundSlot {
	var slot larkInboundSlot
	if raw, ok := larkFieldByNames(fields, dateCols...); ok {
		slot.touched = true
		if t, ok := parseLarkBitableDate(raw); ok {
			slot.HasTime = true
			slot.ScheduledAt = t
		}
	}
	if raw, ok := larkFieldByNames(fields, noteCols...); ok {
		slot.touched = true
		if !larkFieldIsEmpty(raw) {
			slot.Note = larkFieldText(raw)
		}
	}
	if len(meetCols) > 0 {
		if raw, ok := larkFieldByNames(fields, meetCols...); ok {
			slot.touched = true
			if !larkFieldIsEmpty(raw) {
				slot.MeetLink = strings.TrimSpace(larkFieldText(raw))
			}
		}
	}
	if len(typeCols) > 0 {
		if raw, ok := larkFieldByNames(fields, typeCols...); ok {
			if pt, ok := parsePresentTypeField(raw); ok {
				slot.touched = true
				slot.PresentType = pt
			}
		}
	}
	return slot
}

func syncAppointmentsFromLarkSlots(ctx context.Context, projectID, apptType string, slots []larkApptSlot) error {
	if err := deleteAllAppointmentsOfType(ctx, projectID, apptType); err != nil {
		return err
	}
	for _, s := range slots {
		if !s.HasTime {
			continue
		}
		pt, meet := "", ""
		if apptType == "present" {
			pt = s.PresentType
			meet = s.MeetLink
		}
		if err := insertProjectAppointmentRow(ctx, projectID, apptType, "", pt, "", meet, "", s.ScheduledAt, s.Note); err != nil {
			return err
		}
	}
	if apptType == "present" {
		syncLatestPresentLegacyColumns(ctx, projectID)
	}
	return nil
}

func deleteAllAppointmentsOfType(ctx context.Context, projectID, apptType string) error {
	_, err := config.DB.Exec(ctx,
		`DELETE FROM project_appointments WHERE project_id = $1::uuid AND appt_type = $2`,
		projectID, apptType,
	)
	if err != nil {
		return err
	}
	if apptType == "present" {
		_, err = config.DB.Exec(ctx,
			`UPDATE projects
			 SET appointment_date = NULL, appointment_note = NULL, present_type = NULL,
			     calendar_event_id = NULL, last_activity_at = NOW()
			 WHERE id = $1::uuid`, projectID,
		)
	}
	return err
}

func mergeInboundSlots(existing []larkApptSlot, fields map[string]interface{}, apptType string) ([]larkApptSlot, bool) {
	var anyTouched bool
	out := make([]larkApptSlot, 0, maxAppointmentsPerType)
	for n := 1; n <= maxAppointmentsPerType; n++ {
		var dateCols, meetCols, noteCols, typeCols []string
		switch apptType {
		case "present":
			dateCols = larkPresentDateColsN(n)
			meetCols = larkPresentMeetColsN(n)
			noteCols = larkPresentNoteColsN(n)
			typeCols = larkPresentTypeColsN(n)
		case "demo":
			dateCols = larkDemoDateColsN(n)
			noteCols = larkDemoNoteColsN(n)
		case "site_survey":
			dateCols = larkSurveyDateColsN(n)
			noteCols = larkSurveyNoteColsN(n)
		default:
			return nil, false
		}
		in := parseInboundSlot(fields, n, dateCols, meetCols, noteCols, typeCols)
		if in.touched {
			anyTouched = true
		}
		var base larkApptSlot
		if n <= len(existing) {
			base = existing[n-1]
		}
		if !in.touched {
			if base.HasTime {
				out = append(out, base)
			}
			continue
		}
		if in.HasTime {
			base.HasTime = true
			base.ScheduledAt = in.ScheduledAt
		}
		if apptType == "present" {
			if raw, ok := larkFieldByNames(fields, typeCols...); ok {
				if pt, ok := parsePresentTypeField(raw); ok {
					base.PresentType = pt
				}
			}
			if raw, ok := larkFieldByNames(fields, meetCols...); ok {
				if larkFieldIsEmpty(raw) {
					base.MeetLink = ""
				} else {
					base.MeetLink = strings.TrimSpace(larkFieldText(raw))
				}
			}
		}
		if raw, ok := larkFieldByNames(fields, noteCols...); ok {
			if larkFieldIsEmpty(raw) {
				base.Note = ""
			} else {
				base.Note = larkFieldText(raw)
			}
		}
		if base.HasTime {
			out = append(out, base)
		}
	}
	return out, anyTouched
}

func applyLarkInboundAppointments(projectID string, fields map[string]interface{}) error {
	ctx := context.Background()
	existingAll := loadLarkAppointmentsByType(projectID)
	for _, apptType := range []string{"present", "demo", "site_survey"} {
		slots, touched := mergeInboundSlots(existingAll[apptType], fields, apptType)
		if !touched {
			continue
		}
		if err := syncAppointmentsFromLarkSlots(ctx, projectID, apptType, slots); err != nil {
			return fmt.Errorf("%s: %w", apptType, err)
		}
		log.Printf("[LARK] inbound appt slots project=%s type=%s count=%d\n", projectID, apptType, len(slots))
	}
	return nil
}
