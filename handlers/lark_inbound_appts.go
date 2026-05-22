package handlers

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"vizzel-backend/config"
)

type inboundApptChange struct {
	clearDate bool
	setDate   bool
	date      time.Time
	clearNote bool
	setNote   bool
	note      string
	setPresentType bool
	presentType    string
}

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

func parseInboundAppointmentFields(fields map[string]interface{}) map[string]inboundApptChange {
	specs := []struct {
		typ      string
		dateCols []string
		noteCols []string
	}{
		{"present", []string{larkColApptPresentDate, "วันนัดพรีเซ็น"}, []string{larkColPresentNote}},
		{"demo", []string{larkColApptDemoDate, "วันนัด Demo", "วันนัด demo"}, []string{larkColDemoNote}},
		{"site_survey", []string{larkColApptSurveyDate, "วันนัด Site Survey"}, []string{larkColSurveyNote}},
	}

	out := map[string]inboundApptChange{}
	for _, sp := range specs {
		var ch inboundApptChange
		if raw, ok := larkFieldByNames(fields, sp.dateCols...); ok {
			if larkFieldIsEmpty(raw) {
				ch.clearDate = true
			} else if t, ok := parseLarkBitableDate(raw); ok {
				ch.setDate = true
				ch.date = t
			} else {
				ch.clearDate = true
			}
		}
		if raw, ok := larkFieldByNames(fields, sp.noteCols...); ok {
			if larkFieldIsEmpty(raw) {
				ch.clearNote = true
			} else {
				ch.setNote = true
				ch.note = larkFieldText(raw)
			}
		}
		if sp.typ == "present" {
			if raw, ok := fields[larkColPresentType]; ok {
				pt := strings.TrimSpace(larkFieldText(raw))
				if pt == "" || pt == "—" {
					ch.setPresentType = true
					ch.presentType = ""
				} else if pt == "online" || pt == "onsite" {
					ch.setPresentType = true
					ch.presentType = pt
				}
			}
		}
		if ch.clearDate || ch.setDate || ch.clearNote || ch.setNote || ch.setPresentType {
			out[sp.typ] = ch
		}
	}
	return out
}

// replaceAppointmentsFromLark maps one Lark date column to a single appointment row in the app.
func replaceAppointmentsFromLark(ctx context.Context, projectID, apptType string, at time.Time, note, presentType string) error {
	if err := deleteAllAppointmentsOfType(ctx, projectID, apptType); err != nil {
		return err
	}
	if err := insertProjectAppointmentRow(ctx, projectID, apptType, "", presentType, "", at, note); err != nil {
		return err
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

func deleteProjectAppointment(ctx context.Context, projectID, apptType string) error {
	if err := deleteAllAppointmentsOfType(ctx, projectID, apptType); err != nil {
		return err
	}
	log.Printf("[LARK] inbound cleared appt project=%s type=%s\n", projectID, apptType)
	return nil
}

func applyLarkInboundAppointments(projectID string, fields map[string]interface{}) error {
	changes := parseInboundAppointmentFields(fields)
	if len(changes) == 0 {
		return nil
	}
	ctx := context.Background()

	for apptType, ch := range changes {
		if ch.clearDate {
			if err := deleteProjectAppointment(ctx, projectID, apptType); err != nil {
				return fmt.Errorf("clear %s: %w", apptType, err)
			}
			continue
		}

		if ch.setDate {
			note := ""
			if ch.setNote {
				note = ch.note
			}
			pt := ""
			if apptType == "present" && ch.setPresentType {
				pt = ch.presentType
			}
			if err := replaceAppointmentsFromLark(ctx, projectID, apptType, ch.date, note, pt); err != nil {
				return fmt.Errorf("replace %s: %w", apptType, err)
			}
			log.Printf("[LARK] inbound appt set project=%s type=%s\n", projectID, apptType)
			continue
		}

		if ch.clearNote || ch.setNote {
			note := ""
			if ch.setNote {
				note = ch.note
			}
			tag, err := config.DB.Exec(ctx,
				`UPDATE project_appointments SET note = $1, updated_at = NOW()
				 WHERE project_id = $2::uuid AND appt_type = $3`,
				note, projectID, apptType,
			)
			if err != nil {
				return fmt.Errorf("note %s: %w", apptType, err)
			}
			if tag.RowsAffected() == 0 {
				continue
			}
			if apptType == "present" {
				_, _ = config.DB.Exec(ctx,
					`UPDATE projects SET appointment_note = $1, last_activity_at = NOW() WHERE id = $2::uuid`,
					note, projectID,
				)
			}
			log.Printf("[LARK] inbound appt note project=%s type=%s\n", projectID, apptType)
		} else if ch.setPresentType && apptType == "present" {
			_, _ = config.DB.Exec(ctx,
				`UPDATE project_appointments SET present_type = NULLIF($1,''), updated_at = NOW()
				 WHERE project_id = $2::uuid AND appt_type = 'present'`,
				ch.presentType, projectID,
			)
			_, _ = config.DB.Exec(ctx,
				`UPDATE projects SET present_type = NULLIF($1,''), last_activity_at = NOW() WHERE id = $2::uuid`,
				ch.presentType, projectID,
			)
		}
	}
	return nil
}
