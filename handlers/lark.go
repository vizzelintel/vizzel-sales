package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
	"vizzel-backend/models"
)

var (
	larkCachedToken string
	larkTokenExpiry time.Time
	larkHTTPClient  = &http.Client{Timeout: 30 * time.Second}
)

type larkAPIResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func larkBaseURL() string {
	if b := strings.TrimSpace(os.Getenv("LARK_API_BASE")); b != "" {
		return strings.TrimSuffix(b, "/")
	}
	return "https://open.larksuite.com"
}

func getLarkAccessToken() (string, error) {
	if larkCachedToken != "" && time.Now().Before(larkTokenExpiry) {
		return larkCachedToken, nil
	}
	appID := os.Getenv("LARK_APP_ID")
	appSecret := os.Getenv("LARK_APP_SECRET")
	if appID == "" || appSecret == "" {
		return "", fmt.Errorf("LARK credentials not configured")
	}
	body, _ := json.Marshal(map[string]string{"app_id": appID, "app_secret": appSecret})
	resp, err := larkHTTPClient.Post(
		larkBaseURL()+"/open-apis/auth/v3/tenant_access_token/internal",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	var res struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.Unmarshal(rb, &res); err != nil {
		return "", fmt.Errorf("Lark auth decode: %w", err)
	}
	if res.Code != 0 {
		return "", fmt.Errorf("Lark auth failed code=%d msg=%s", res.Code, res.Msg)
	}
	larkCachedToken = res.TenantAccessToken
	larkTokenExpiry = time.Now().Add(time.Duration(res.Expire-60) * time.Second)
	return larkCachedToken, nil
}

var larkStatusLabels = map[string]string{
	"register":  "Register",
	"present":   "Present",
	"quotation": "Quotation",
	"tor":       "TOR",
	"contract":  "Contract",
	"closed":    "Closed",
	"reject":    "Reject",
}

func getLarkStatusLabel(status string) string {
	if label, ok := larkStatusLabels[status]; ok {
		return label
	}
	return status
}

func larkCreatedAtValue(t time.Time) interface{} {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LARK_DATE_FORMAT"))) {
	case "text", "string":
		return t.Format("2006-01-02")
	case "omit", "skip", "none":
		return nil
	default:
		// Lark Date / CreatedTime fields expect Unix ms.
		return t.UnixMilli()
	}
}

func buildLarkFields(p models.Project, companyName string) map[string]interface{} {
	fields := map[string]interface{}{
		"ชื่อหน่วยงาน":   p.AgencyName,
		"ประเภทหน่วยงาน": p.AgencyType,
		"จังหวัด":        p.Region,
		"ผู้ติดต่อ":      p.ContactPerson,
		"โทรศัพท์":       p.ContactPhone,
		"บริษัท Dealer":  companyName,
		"สถานะ":          getLarkStatusLabel(p.Status),
		"Project ID":     p.ID,
	}
	if v := larkCreatedAtValue(p.CreatedAt); v != nil {
		fields["วันที่สร้าง"] = v
	}
	return fields
}

func larkDo(method, url string, token string, payload interface{}) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := larkHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return rb, fmt.Errorf("Lark HTTP %d: %s", resp.StatusCode, string(rb))
	}
	var api larkAPIResp
	if err := json.Unmarshal(rb, &api); err != nil {
		return rb, fmt.Errorf("Lark response decode: %w body=%s", err, string(rb))
	}
	if api.Code != 0 {
		return rb, fmt.Errorf("Lark API code=%d msg=%s", api.Code, api.Msg)
	}
	return rb, nil
}

func findLarkRecord(token, appToken, tableID, projectID string) (string, error) {
	url := fmt.Sprintf(
		"%s/open-apis/bitable/v1/apps/%s/tables/%s/records/search",
		larkBaseURL(), appToken, tableID,
	)
	rb, err := larkDo("POST", url, token, map[string]interface{}{
		"filter": map[string]interface{}{
			"conjunction": "and",
			"conditions": []map[string]interface{}{
				{"field_name": "Project ID", "operator": "is", "value": []string{projectID}},
			},
		},
		"page_size": 1,
	})
	if err != nil {
		return "", err
	}
	var res struct {
		Data struct {
			Items []struct {
				RecordID string `json:"record_id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rb, &res); err != nil {
		return "", err
	}
	if len(res.Data.Items) > 0 {
		return res.Data.Items[0].RecordID, nil
	}
	return "", nil
}

func createLarkRecord(token, appToken, tableID string, fields map[string]interface{}) (string, error) {
	url := fmt.Sprintf(
		"%s/open-apis/bitable/v1/apps/%s/tables/%s/records",
		larkBaseURL(), appToken, tableID,
	)
	rb, err := larkDo("POST", url, token, map[string]interface{}{"fields": fields})
	if err != nil {
		log.Printf("[LARK] create failed: %v body=%s\n", err, rb)
		return "", err
	}
	var res struct {
		Data struct {
			Record struct {
				RecordID string `json:"record_id"`
			} `json:"record"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rb, &res); err != nil {
		return "", err
	}
	return res.Data.Record.RecordID, nil
}

func updateLarkRecord(token, appToken, tableID, recordID string, fields map[string]interface{}) error {
	url := fmt.Sprintf(
		"%s/open-apis/bitable/v1/apps/%s/tables/%s/records/%s",
		larkBaseURL(), appToken, tableID, recordID,
	)
	rb, err := larkDo("PUT", url, token, map[string]interface{}{"fields": fields})
	if err != nil {
		log.Printf("[LARK] update failed record=%s: %v body=%s\n", recordID, err, rb)
		return err
	}
	return nil
}

func loadLarkRecordID(projectID string) string {
	var rid string
	err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(lark_record_id,'') FROM projects WHERE id = $1::uuid`, projectID,
	).Scan(&rid)
	if err != nil {
		return ""
	}
	return rid
}

func saveLarkRecordID(projectID, recordID string) {
	if recordID == "" {
		return
	}
	_, err := config.DB.Exec(context.Background(),
		`UPDATE projects SET lark_record_id = $1 WHERE id = $2::uuid`, recordID, projectID,
	)
	if err != nil && strings.Contains(err.Error(), "lark_record_id") {
		// Column not migrated yet — sync still works via search.
		return
	}
	if err != nil {
		log.Printf("[LARK] save record_id failed project=%s: %v\n", projectID, err)
	}
}

// SyncProjectToLark upserts a single project record into Lark Bitable.
// Safe to call in a goroutine; logs errors instead of returning them.
func SyncProjectToLark(p models.Project) {
	if shouldSkipLarkPush(p.ID) {
		return
	}
	if err := syncProjectToLarkCore(p); err != nil {
		log.Printf("[LARK] sync failed project=%s: %v\n", p.ID, err)
	}
}

// syncProjectToLarkCore performs the upsert and returns any error (for admin probes).
func syncProjectToLarkCore(p models.Project) error {
	tenantToken, err := getLarkAccessToken()
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	cfg, err := resolveLarkBitableConfig(tenantToken)
	if err != nil {
		return err
	}

	companyName := ""
	if p.CompanyID != "" {
		_ = config.DB.QueryRow(context.Background(),
			`SELECT COALESCE(name,'') FROM companies WHERE id = $1::uuid`, p.CompanyID,
		).Scan(&companyName)
	}

	baseFields := buildLarkFields(p, companyName)
	extras := buildLarkExtras(p.ID, p.DetailNote)
	recordID := strings.TrimSpace(p.LarkRecordID)
	if recordID == "" {
		recordID = loadLarkRecordID(p.ID)
	}
	if recordID == "" {
		recordID, err = findLarkRecord(tenantToken, cfg.AppToken, cfg.TableID, p.ID)
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}
	}

	if recordID == "" {
		fullFields := larkMergeFields(baseFields, extras)
		recordID, err = createLarkRecord(tenantToken, cfg.AppToken, cfg.TableID, fullFields)
		if err != nil && isLarkUnknownFieldErr(err) {
			log.Printf("[LARK] create with extras failed, retrying base: %v\n", err)
			recordID, err = createLarkRecord(tenantToken, cfg.AppToken, cfg.TableID, baseFields)
		}
		if err != nil {
			return fmt.Errorf("create: %w", err)
		}
		saveLarkRecordID(p.ID, recordID)
		syncLarkFieldsGradual(tenantToken, cfg.AppToken, cfg.TableID, recordID, extras)
		log.Printf("[LARK] created: %s record_id=%s\n", p.AgencyName, recordID)
		return nil
	}

	if err := updateLarkRecord(tenantToken, cfg.AppToken, cfg.TableID, recordID, baseFields); err != nil {
		return fmt.Errorf("update base: %w", err)
	}
	syncLarkFieldsGradual(tenantToken, cfg.AppToken, cfg.TableID, recordID, extras)
	saveLarkRecordID(p.ID, recordID)
	log.Printf("[LARK] updated: %s → %s\n", p.AgencyName, p.Status)
	return nil
}

func larkMergeFields(base, extras map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(base)+len(extras))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extras {
		out[k] = v
	}
	return out
}

// SyncAllProjectsToLark is a gin handler (admin only) that bulk-syncs all
// projects to Lark Bitable in a background goroutine.
func SyncAllProjectsToLark(c *gin.Context) {
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)
	var role string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,'') FROM users WHERE id = $1::uuid`, uid,
	).Scan(&role); err != nil || role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin เท่านั้น"})
		return
	}

	var n int
	_ = config.DB.QueryRow(context.Background(), `SELECT COUNT(*) FROM projects`).Scan(&n)

	go func() {
		if res, err := RunBulkLarkSync(); err != nil {
			log.Printf("[LARK] bulk sync error: %v\n", err)
		} else if len(res.Failed) > 0 {
			log.Printf("[LARK] bulk sync partial: %d/%d ok\n", res.Success, res.Total)
		}
	}()

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("กำลัง sync %d โครงการไปยัง Lark", n),
	})
}

// LarkDiagnose (admin) checks Lark auth and returns the last API error hint.
func LarkDiagnose(c *gin.Context) {
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)
	var role string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,'') FROM users WHERE id = $1::uuid`, uid,
	).Scan(&role); err != nil || role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin เท่านั้น"})
		return
	}

	out := gin.H{
		"api_base":          larkBaseURL(),
		"app_id_set":        os.Getenv("LARK_APP_ID") != "",
		"app_secret_set":    os.Getenv("LARK_APP_SECRET") != "",
		"app_token_env_set": os.Getenv("LARK_BASE_APP_TOKEN") != "",
		"wiki_node_set":     os.Getenv("LARK_WIKI_NODE_TOKEN") != "",
		"table_id":          os.Getenv("LARK_TABLE_ID"),
		"date_format":       strings.TrimSpace(os.Getenv("LARK_DATE_FORMAT")),
	}
	if out["date_format"] == "" {
		out["date_format"] = "ms (default)"
	}

	d := RunLarkDiagnose()
	out["auth_ok"] = d.AuthOK
	if d.AuthError != "" {
		out["auth_error"] = d.AuthError
	}
	out["config_ok"] = d.ConfigOK
	if d.ConfigError != "" {
		out["config_error"] = d.ConfigError
	}
	if d.AppTokenSource != "" {
		out["app_token_source"] = d.AppTokenSource
	}
	if d.AppTokenPrefix != "" {
		out["app_token_prefix"] = d.AppTokenPrefix
	}
	if d.TableID != "" {
		out["table_id"] = d.TableID
	}
	out["fields_ok"] = d.FieldsOK
	if d.FieldsError != "" {
		out["fields_error"] = d.FieldsError
	}
	out["field_count"] = d.FieldCount
	out["field_names"] = d.FieldNames
	out["records_ok"] = d.RecordsOK
	if d.RecordsError != "" {
		out["records_error"] = d.RecordsError
	}
	out["records_total_hint"] = d.RecordsTotal
	out["recommended_columns"] = RecommendedLarkColumns()
	out["extras_enabled"] = larkExtrasEnabled()

	c.JSON(http.StatusOK, out)
}

// LarkSyncProbe (admin) syncs one project to Lark synchronously and returns the result.
func LarkSyncProbe(c *gin.Context) {
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)
	var role string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,'') FROM users WHERE id = $1::uuid`, uid,
	).Scan(&role); err != nil || role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin เท่านั้น"})
		return
	}

	projectID := strings.TrimSpace(c.Query("project_id"))
	if projectID == "" {
		_ = config.DB.QueryRow(context.Background(),
			`SELECT id::text FROM projects ORDER BY created_at DESC LIMIT 1`,
		).Scan(&projectID)
	}
	if projectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no projects in database"})
		return
	}

	p, err := scanProject(config.DB.QueryRow(context.Background(),
		`SELECT `+projectCols+` FROM projects WHERE id = $1::uuid`, projectID,
	))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}

	if syncErr := syncProjectToLarkCore(p); syncErr != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":         false,
			"project_id": projectID,
			"agency":     p.AgencyName,
			"error":      syncErr.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"project_id": projectID,
		"agency":     p.AgencyName,
		"message":    "synced to Lark",
	})
}
