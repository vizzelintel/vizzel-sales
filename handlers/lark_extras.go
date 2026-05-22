package handlers

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"vizzel-backend/config"
)

// Lark Bitable column names for appointments & documents (add these to your table).
const (
	larkColApptPresentDate  = "วันพรีเซ็น"
	larkColPresentType      = "รูปแบบ Present"
	larkColPresentNote      = "หมายเหตุ Present"
	larkColPresentMeetLink  = "ลิงก์ Google Meet"
	larkColApptDemoDate     = "วัน Demo"
	larkColDemoNote         = "หมายเหตุ Demo"
	larkColApptSurveyDate   = "วัน Site Survey"
	larkColSurveyNote       = "หมายเหตุ Site Survey"
	larkColApptSummary      = "สรุปนัดหมาย"
	larkColApptGuide        = "วิธีใช้ (Support)"
	larkColDocuments        = "เอกสาร"
	larkColDetailNote       = "รายละเอียดเพิ่มเติม"
)

func larkDetailNoteColumnName() string {
	if c := strings.TrimSpace(os.Getenv("LARK_COL_DETAIL_NOTE")); c != "" {
		return c
	}
	return larkColDetailNote
}

var larkDocLabels = map[string]string{
	"quotation_support": "ใบเสนอราคา (Support)",
	"quotation_dealer":  "ใบเสนอราคา (Dealer)",
	"tor_support":       "ร่าง TOR (Support)",
	"tor_dealer":        "ร่าง TOR (Dealer)",
	"contract":          "เอกสารสัญญา",
	"closing":           "เอกสารปิดงาน",
	"site_survey":       "เอกสาร Site Survey",
}

var larkApptLabels = map[string]string{
	"present":     "Present",
	"demo":        "Demo",
	"site_survey": "Site Survey",
}

type larkApptSlot struct {
	ScheduledAt time.Time
	Note        string
	PresentType string
	MeetLink    string
	HasTime     bool
}

func larkExtrasEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LARK_SYNC_EXTRAS"))) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func larkBangkok() *time.Location {
	loc, err := time.LoadLocation("Asia/Bangkok")
	if err != nil {
		return time.FixedZone("ICT", 7*3600)
	}
	return loc
}

func parseLarkTimestamp(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05-07",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func formatLarkDisplayTime(t time.Time) string {
	return t.In(larkBangkok()).Format("02/01/2006 15:04")
}

func larkDateFieldValue(t time.Time) interface{} {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LARK_DATE_FORMAT"))) {
	case "text", "string":
		return t.In(larkBangkok()).Format("2006-01-02 15:04")
	default:
		return t.UnixMilli()
	}
}

func loadLarkAppointmentsByType(projectID string) map[string][]larkApptSlot {
	out := map[string][]larkApptSlot{}
	rows, err := config.DB.Query(context.Background(),
		`SELECT appt_type, scheduled_at::text, COALESCE(note,''), COALESCE(present_type,''), COALESCE(meet_link,'')
		 FROM project_appointments WHERE project_id = $1::uuid
		 ORDER BY appt_type, scheduled_at ASC`,
		projectID,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var typ, at, note, pt, meet string
			if rows.Scan(&typ, &at, &note, &pt, &meet) == nil {
				if t, ok := parseLarkTimestamp(at); ok {
					out[typ] = append(out[typ], larkApptSlot{ScheduledAt: t, Note: note, PresentType: pt, MeetLink: meet, HasTime: true})
				}
			}
		}
	}

	if len(out["present"]) == 0 {
		var legacyAt, legacyNote, legacyPresent string
		_ = config.DB.QueryRow(context.Background(),
			`SELECT COALESCE(appointment_date::text,''), COALESCE(appointment_note,''), COALESCE(present_type,'')
			 FROM projects WHERE id = $1::uuid`, projectID,
		).Scan(&legacyAt, &legacyNote, &legacyPresent)
		if t, ok := parseLarkTimestamp(legacyAt); ok {
			out["present"] = []larkApptSlot{{ScheduledAt: t, Note: legacyNote, PresentType: legacyPresent, HasTime: true}}
		}
	}
	return out
}

func latestLarkApptSlot(slots []larkApptSlot) (larkApptSlot, bool) {
	if len(slots) == 0 {
		return larkApptSlot{}, false
	}
	return slots[len(slots)-1], true
}

type larkDocRow struct {
	DocType  string
	FileName string
	FileURL  string
}

func loadLarkDocuments(projectID string) []larkDocRow {
	rows, err := config.DB.Query(context.Background(),
		`SELECT COALESCE(doc_type,''), COALESCE(file_name,''), COALESCE(file_url,'')
		 FROM documents WHERE project_id = $1::uuid ORDER BY created_at ASC`,
		projectID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var list []larkDocRow
	for rows.Next() {
		var d larkDocRow
		if rows.Scan(&d.DocType, &d.FileName, &d.FileURL) == nil && d.FileURL != "" {
			list = append(list, d)
		}
	}
	return list
}

func setLarkSlotFields(fields map[string]interface{}, n int, slot larkApptSlot, apptType string) {
	clear := func(cols ...string) {
		for _, c := range cols {
			fields[c] = ""
		}
	}
	switch apptType {
	case "present":
		if slot.HasTime {
			fields[larkColPresentDateN(n)] = larkDateFieldValue(slot.ScheduledAt)
			fields[larkColPresentNoteN(n)] = strings.TrimSpace(slot.Note)
			fields[larkColPresentMeetN(n)] = strings.TrimSpace(slot.MeetLink)
			if slot.PresentType != "" {
				fields[larkColPresentTypeN(n)] = slot.PresentType
			} else {
				fields[larkColPresentTypeN(n)] = ""
			}
		} else {
			clear(larkColPresentDateN(n), larkColPresentNoteN(n), larkColPresentMeetN(n), larkColPresentTypeN(n))
		}
		if n == 1 {
			if slot.HasTime {
				fields[larkColApptPresentDate] = larkDateFieldValue(slot.ScheduledAt)
				fields[larkColPresentNote] = strings.TrimSpace(slot.Note)
				fields[larkColPresentMeetLink] = strings.TrimSpace(slot.MeetLink)
				if slot.PresentType != "" {
					fields[larkColPresentType] = slot.PresentType
				}
			} else {
				clear(larkColApptPresentDate, larkColPresentNote, larkColPresentMeetLink, larkColPresentType)
			}
		}
	case "demo":
		if slot.HasTime {
			fields[larkColDemoDateN(n)] = larkDateFieldValue(slot.ScheduledAt)
			fields[larkColDemoNoteN(n)] = strings.TrimSpace(slot.Note)
		} else {
			clear(larkColDemoDateN(n), larkColDemoNoteN(n))
		}
		if n == 1 && slot.HasTime {
			fields[larkColApptDemoDate] = larkDateFieldValue(slot.ScheduledAt)
			fields[larkColDemoNote] = strings.TrimSpace(slot.Note)
		} else if n == 1 {
			clear(larkColApptDemoDate, larkColDemoNote)
		}
	case "site_survey":
		if slot.HasTime {
			fields[larkColSurveyDateN(n)] = larkDateFieldValue(slot.ScheduledAt)
			fields[larkColSurveyNoteN(n)] = strings.TrimSpace(slot.Note)
		} else {
			clear(larkColSurveyDateN(n), larkColSurveyNoteN(n))
		}
		if n == 1 && slot.HasTime {
			fields[larkColApptSurveyDate] = larkDateFieldValue(slot.ScheduledAt)
			fields[larkColSurveyNote] = strings.TrimSpace(slot.Note)
		} else if n == 1 {
			clear(larkColApptSurveyDate, larkColSurveyNote)
		}
	}
}

func buildLarkAppointmentFields(projectID string) map[string]interface{} {
	appts := loadLarkAppointmentsByType(projectID)
	fields := map[string]interface{}{}

	for _, typ := range []string{"present", "demo", "site_survey"} {
		slots := appts[typ]
		for n := 1; n <= maxAppointmentsPerType; n++ {
			var slot larkApptSlot
			if n <= len(slots) {
				slot = slots[n-1]
			}
			setLarkSlotFields(fields, n, slot, typ)
		}
	}

	var summaryLines []string
	for _, typ := range []string{"present", "demo", "site_survey"} {
		slots := appts[typ]
		label := larkApptLabels[typ]
		if len(slots) == 0 {
			summaryLines = append(summaryLines, fmt.Sprintf("%s: —", label))
			continue
		}
		for i, s := range slots {
			if !s.HasTime {
				continue
			}
			line := fmt.Sprintf("%s #%d (คอลัมน์ %d): %s", label, i+1, i+1, formatLarkDisplayTime(s.ScheduledAt))
			if typ == "present" && s.PresentType != "" {
				line += " (" + s.PresentType + ")"
			}
			if strings.TrimSpace(s.Note) != "" {
				line += " — " + strings.TrimSpace(s.Note)
			}
			if typ == "present" && strings.TrimSpace(s.MeetLink) != "" {
				line += "\n   Meet: " + strings.TrimSpace(s.MeetLink)
			}
			summaryLines = append(summaryLines, line)
		}
		if len(slots) > 0 {
			summaryLines = append(summaryLines, fmt.Sprintf("  (%s รวม %d/%d ครั้ง)", label, len(slots), maxAppointmentsPerType))
		}
	}
	header := fmt.Sprintf("สรุปจากแอป (อ่านอย่างเดียว) — แก้นัดที่คอลัมน์ 1–%d\n", maxAppointmentsPerType)
	fields[larkColApptSummary] = header + strings.Join(summaryLines, "\n")
	return fields
}

func buildLarkApptGuideField() map[string]interface{} {
	text := strings.Join([]string{
		"【Support — นัดหมาย 1–10】",
		"• ครั้งที่ 1–10: ใช้คอลัมน์ วันพรีเซ็น N + Meet พรีเซ็น N + หมายเหตุ/รูปแบบ N",
		"• ครั้งที่ N ในแอป = คอลัมน์ชุดที่ N ใน Lark (เรียงตามวันที่ในแอป)",
		"• สรุปนัดหมาย = อ่านอย่างเดียว อย่าแก้",
		"• ลบนัด: ล้างวันที่ในคอลัมน์นั้น แล้วบันทึก",
	}, "\n")
	return map[string]interface{}{larkColApptGuide: text}
}

func buildLarkDetailNoteField(detailNote string) map[string]interface{} {
	note := strings.TrimSpace(detailNote)
	if note == "" {
		note = "—"
	}
	return map[string]interface{}{larkDetailNoteColumnName(): note}
}

// buildLarkExtras returns optional Bitable columns (appointments, documents, detail note).
func buildLarkExtras(projectID, detailNote string) map[string]interface{} {
	if !larkExtrasEnabled() {
		return nil
	}
	extras := make(map[string]interface{})
	for k, v := range buildLarkDetailNoteField(detailNote) {
		extras[k] = v
	}
	for k, v := range buildLarkAppointmentFields(projectID) {
		extras[k] = v
	}
	for k, v := range buildLarkApptGuideField() {
		extras[k] = v
	}
	for k, v := range buildLarkDocumentFields(projectID) {
		extras[k] = v
	}
	return extras
}

// syncLarkFieldsGradual updates one column at a time so a missing column does not block others.
func syncLarkFieldsGradual(token, appToken, tableID, recordID string, fields map[string]interface{}) {
	if recordID == "" || len(fields) == 0 {
		return
	}
	for col, val := range fields {
		err := updateLarkRecord(token, appToken, tableID, recordID, map[string]interface{}{col: val})
		if err == nil {
			continue
		}
		if isLarkUnknownFieldErr(err) {
			log.Printf("[LARK] column %q not in Bitable, skipped\n", col)
			continue
		}
		log.Printf("[LARK] sync column %q failed: %v\n", col, err)
	}
}

func buildLarkDocumentFields(projectID string) map[string]interface{} {
	docs := loadLarkDocuments(projectID)
	var lines []string
	for _, d := range docs {
		label := larkDocLabels[d.DocType]
		if label == "" {
			label = d.DocType
		}
		name := strings.TrimSpace(d.FileName)
		if name == "" {
			name = "ไฟล์"
		}
		lines = append(lines, fmt.Sprintf("%s: %s\n%s", label, name, d.FileURL))
	}
	text := strings.Join(lines, "\n\n")
	if text == "" {
		text = "—"
	}
	return map[string]interface{}{larkColDocuments: text}
}

func isLarkUnknownFieldErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "FieldNameNotFound") ||
		strings.Contains(msg, "field_name_not_found") ||
		strings.Contains(msg, "1254044")
}

// SyncProjectToLarkByID reloads the project and syncs to Lark (safe for goroutines).
func SyncProjectToLarkByID(projectID string) {
	if strings.TrimSpace(projectID) == "" {
		return
	}
	p, err := scanProject(config.DB.QueryRow(context.Background(),
		`SELECT `+projectCols+` FROM projects WHERE id = $1::uuid`, projectID,
	))
	if err != nil {
		log.Printf("[LARK] load project %s: %v\n", projectID, err)
		return
	}
	SyncProjectToLark(p)
}

// RecommendedLarkColumns lists Bitable columns for full appointment/document sync.
func RecommendedLarkColumns() []string {
	cols := []string{
		"ชื่อหน่วยงาน", "ประเภทหน่วยงาน", "จังหวัด", "ผู้ติดต่อ", "โทรศัพท์",
		"บริษัท Dealer", "สถานะ", larkDetailNoteColumnName(), "Project ID", "วันที่สร้าง",
		larkColApptPresentDate, larkColPresentType, larkColPresentNote, larkColPresentMeetLink,
		larkColApptDemoDate, larkColDemoNote,
		larkColApptSurveyDate, larkColSurveyNote,
		larkColApptSummary, larkColApptGuide, larkColDocuments,
	}
	return append(cols, RecommendedLarkAppointmentSlotColumns()...)
}
