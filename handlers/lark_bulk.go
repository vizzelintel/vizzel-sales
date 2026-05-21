package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"vizzel-backend/config"
	"vizzel-backend/models"
)

// LarkDiagnoseResult is returned by RunLarkDiagnose for CLI/admin tooling.
type LarkDiagnoseResult struct {
	AuthOK         bool     `json:"auth_ok"`
	AuthError      string   `json:"auth_error,omitempty"`
	ConfigOK       bool     `json:"config_ok"`
	ConfigError    string   `json:"config_error,omitempty"`
	AppTokenSource string   `json:"app_token_source,omitempty"`
	AppTokenPrefix string   `json:"app_token_prefix,omitempty"`
	TableID        string   `json:"table_id,omitempty"`
	FieldsOK       bool     `json:"fields_ok"`
	FieldsError    string   `json:"fields_error,omitempty"`
	FieldCount     int      `json:"field_count"`
	FieldNames     []string `json:"field_names,omitempty"`
	RecordsOK      bool     `json:"records_ok"`
	RecordsError   string   `json:"records_error,omitempty"`
	RecordsTotal   int      `json:"records_total_hint"`
}

// RunLarkDiagnose checks Lark credentials and table access without HTTP.
func RunLarkDiagnose() LarkDiagnoseResult {
	out := LarkDiagnoseResult{}
	tenantToken, err := getLarkAccessToken()
	if err != nil {
		out.AuthError = err.Error()
		return out
	}
	out.AuthOK = true

	cfg, err := resolveLarkBitableConfig(tenantToken)
	if err != nil {
		out.ConfigError = err.Error()
		return out
	}
	out.ConfigOK = true
	out.AppTokenSource = cfg.AppTokenSource
	out.AppTokenPrefix = truncToken(cfg.AppToken)
	out.TableID = cfg.TableID

	url := fmt.Sprintf("%s/open-apis/bitable/v1/apps/%s/tables/%s/fields?page_size=100",
		larkBaseURL(), cfg.AppToken, cfg.TableID)
	rb, err := larkDo("GET", url, tenantToken, nil)
	if err != nil {
		out.FieldsError = err.Error()
		return out
	}
	var fieldsRes struct {
		Data struct {
			Items []struct {
				FieldName string `json:"field_name"`
			} `json:"items"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rb, &fieldsRes)
	out.FieldsOK = true
	out.FieldCount = len(fieldsRes.Data.Items)
	for _, it := range fieldsRes.Data.Items {
		out.FieldNames = append(out.FieldNames, it.FieldName)
	}

	searchURL := fmt.Sprintf("%s/open-apis/bitable/v1/apps/%s/tables/%s/records/search",
		larkBaseURL(), cfg.AppToken, cfg.TableID)
	if srb, serr := larkDo("POST", searchURL, tenantToken, map[string]interface{}{"page_size": 1}); serr != nil {
		out.RecordsError = serr.Error()
	} else {
		var sres struct {
			Data struct {
				Total int `json:"total"`
			} `json:"data"`
		}
		_ = json.Unmarshal(srb, &sres)
		out.RecordsOK = true
		out.RecordsTotal = sres.Data.Total
	}
	return out
}

// BulkSyncResult summarizes a bulk Lark sync run.
type BulkSyncResult struct {
	Total   int      `json:"total"`
	Success int      `json:"success"`
	Failed  []string `json:"failed,omitempty"`
}

// RunBulkLarkSync syncs every project to Lark synchronously (for CLI/cron).
func RunBulkLarkSync() (BulkSyncResult, error) {
	rows, err := config.DB.Query(context.Background(),
		`SELECT `+projectCols+` FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return BulkSyncResult{}, err
	}
	defer rows.Close()

	var projects []models.Project
	for rows.Next() {
		if p, err := scanProject(rows); err == nil {
			projects = append(projects, p)
		}
	}
	rows.Close()

	res := BulkSyncResult{Total: len(projects)}
	for _, p := range projects {
		if err := syncProjectToLarkCore(p); err != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s (%s): %v", p.AgencyName, p.ID, err))
			log.Printf("[LARK] bulk fail: %s: %v\n", p.ID, err)
		} else {
			res.Success++
		}
		time.Sleep(100 * time.Millisecond)
	}
	log.Printf("[LARK] Bulk sync done: %d/%d ok\n", res.Success, res.Total)
	return res, nil
}
