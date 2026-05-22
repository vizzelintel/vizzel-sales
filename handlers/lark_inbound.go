package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"vizzel-backend/config"
)

// inboundPushSkip prevents App→Lark echo right after Lark→App updates.
var inboundPushSkip sync.Map

func markInboundSync(projectID string) {
	if projectID == "" {
		return
	}
	inboundPushSkip.Store(projectID, time.Now().Add(45*time.Second))
}

func shouldSkipLarkPush(projectID string) bool {
	if v, ok := inboundPushSkip.Load(projectID); ok {
		if until, ok := v.(time.Time); ok && time.Now().Before(until) {
			return true
		}
		inboundPushSkip.Delete(projectID)
	}
	return false
}

var larkLabelToStatus = map[string]string{
	"register":  "register",
	"present":   "present",
	"quotation": "quotation",
	"tor":       "tor",
	"contract":  "contract",
	"closed":    "closed",
	"reject":    "reject",
	// Thai labels sometimes used in Bitable
	"ลงทะเบียน": "register",
}

func statusFromLarkLabel(label string) string {
	s := strings.TrimSpace(label)
	if s == "" || s == "—" {
		return ""
	}
	if st, ok := larkLabelToStatus[strings.ToLower(s)]; ok {
		return st
	}
	for k, v := range larkStatusLabels {
		if strings.EqualFold(v, s) {
			return k
		}
	}
	return ""
}

func larkFieldText(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%v", x)
	case int:
		return fmt.Sprintf("%d", x)
	case int64:
		return fmt.Sprintf("%d", x)
	case []interface{}:
		var parts []string
		for _, it := range x {
			if t := larkFieldText(it); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]interface{}:
		if t, ok := x["text"].(string); ok {
			return strings.TrimSpace(t)
		}
		if t, ok := x["name"].(string); ok {
			return strings.TrimSpace(t)
		}
		if t, ok := x["value"].(string); ok {
			return strings.TrimSpace(t)
		}
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func getLarkBitableRecord(token, appToken, tableID, recordID string) (map[string]interface{}, error) {
	url := fmt.Sprintf(
		"%s/open-apis/bitable/v1/apps/%s/tables/%s/records/%s",
		larkBaseURL(), appToken, tableID, recordID,
	)
	rb, err := larkDo("GET", url, token, nil)
	if err != nil {
		return nil, err
	}
	var res struct {
		Data struct {
			Record struct {
				RecordID string                 `json:"record_id"`
				Fields   map[string]interface{} `json:"fields"`
			} `json:"record"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rb, &res); err != nil {
		return nil, fmt.Errorf("decode record: %w", err)
	}
	return res.Data.Record.Fields, nil
}

type larkInboundPatch struct {
	ProjectID       string
	LarkRecordID    string
	AgencyName      string
	AgencyType      string
	Region          string
	ContactPerson   string
	ContactPhone    string
	Status          string
	DetailNote      string
	SetDetailNote   bool
	SetStatus       bool
}

func parseLarkFieldsToPatch(fields map[string]interface{}) larkInboundPatch {
	colDetail := larkDetailNoteColumnName()
	p := larkInboundPatch{}
	p.AgencyName = larkFieldText(fields["ชื่อหน่วยงาน"])
	p.AgencyType = larkFieldText(fields["ประเภทหน่วยงาน"])
	p.Region = larkFieldText(fields["จังหวัด"])
	p.ContactPerson = larkFieldText(fields["ผู้ติดต่อ"])
	p.ContactPhone = larkFieldText(fields["โทรศัพท์"])
	p.ProjectID = larkFieldText(fields["Project ID"])
	if st := statusFromLarkLabel(larkFieldText(fields["สถานะ"])); st != "" {
		p.Status = st
		p.SetStatus = true
	}
	if raw, ok := fields[colDetail]; ok {
		note := larkFieldText(raw)
		if note == "—" {
			note = ""
		}
		p.DetailNote = note
		p.SetDetailNote = true
	}
	return p
}

func findProjectIDByLarkRecord(recordID string) (string, error) {
	var id string
	err := config.DB.QueryRow(context.Background(),
		`SELECT id::text FROM projects WHERE lark_record_id = $1 LIMIT 1`, recordID,
	).Scan(&id)
	return id, err
}

func applyLarkInboundPatch(patch larkInboundPatch, larkRecordID string) error {
	projectID := strings.TrimSpace(patch.ProjectID)
	if projectID == "" && larkRecordID != "" {
		var err error
		projectID, err = findProjectIDByLarkRecord(larkRecordID)
		if err != nil {
			return fmt.Errorf("lookup by lark_record_id: %w", err)
		}
	}
	if projectID == "" {
		return fmt.Errorf("no project linked (missing Project ID / lark_record_id)")
	}

	markInboundSync(projectID)

	var sets []string
	var args []any
	n := 0
	add := func(col, val string) {
		n++
		sets = append(sets, fmt.Sprintf("%s = $%d", col, n))
		args = append(args, val)
	}

	if patch.AgencyName != "" {
		add("agency_name", patch.AgencyName)
	}
	if patch.AgencyType != "" {
		add("agency_type", patch.AgencyType)
	}
	if patch.Region != "" {
		add("region", patch.Region)
	}
	if patch.ContactPerson != "" {
		add("contact_person", patch.ContactPerson)
	}
	if patch.ContactPhone != "" {
		add("contact_phone", patch.ContactPhone)
	}
	if patch.SetDetailNote {
		add("detail_note", patch.DetailNote)
	}
	if patch.SetStatus && patch.Status != "" {
		if !validStatuses[patch.Status] {
			return fmt.Errorf("invalid status from Lark: %s", patch.Status)
		}
		add("status", patch.Status)
	}
	if larkRecordID != "" {
		n++
		sets = append(sets, fmt.Sprintf("lark_record_id = $%d", n))
		args = append(args, larkRecordID)
	}
	if len(sets) == 0 {
		return nil
	}

	sets = append(sets, "last_activity_at = NOW()")
	n++
	args = append(args, projectID)
	q := fmt.Sprintf(`UPDATE projects SET %s WHERE id = $%d::uuid`, strings.Join(sets, ", "), n)
	tag, err := config.DB.Exec(context.Background(), q, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("project not found: %s", projectID)
	}
	log.Printf("[LARK] inbound applied project=%s lark_record=%s\n", projectID, larkRecordID)
	return nil
}

func processLarkBitableRecordChange(tableID, recordID, action string) {
	action = strings.TrimSpace(action)
	if action != "record_edited" && action != "record_added" {
		return
	}
	wantTable := strings.TrimSpace(os.Getenv("LARK_TABLE_ID"))
	if wantTable != "" && tableID != "" && tableID != wantTable {
		return
	}

	token, err := getLarkAccessToken()
	if err != nil {
		log.Printf("[LARK] inbound auth: %v\n", err)
		return
	}
	cfg, err := resolveLarkBitableConfig(token)
	if err != nil {
		log.Printf("[LARK] inbound config: %v\n", err)
		return
	}

	fields, err := getLarkBitableRecord(token, cfg.AppToken, cfg.TableID, recordID)
	if err != nil {
		log.Printf("[LARK] inbound fetch record %s: %v\n", recordID, err)
		return
	}
	patch := parseLarkFieldsToPatch(fields)
	if err := applyLarkInboundPatch(patch, recordID); err != nil {
		log.Printf("[LARK] inbound apply record %s: %v\n", recordID, err)
	}
}
