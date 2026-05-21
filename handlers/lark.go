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
	"time"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
	"vizzel-backend/models"
)

var larkCachedToken string
var larkTokenExpiry time.Time

func getLarkAccessToken() (string, error) {
	if larkCachedToken != "" && time.Now().Before(larkTokenExpiry) {
		return larkCachedToken, nil
	}
	appID     := os.Getenv("LARK_APP_ID")
	appSecret := os.Getenv("LARK_APP_SECRET")
	if appID == "" || appSecret == "" {
		return "", fmt.Errorf("LARK credentials not configured")
	}
	body, _ := json.Marshal(map[string]string{"app_id": appID, "app_secret": appSecret})
	resp, err := http.Post(
		"https://open.larksuite.com/open-apis/auth/v3/tenant_access_token/internal",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var res struct {
		Code              int    `json:"code"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	json.NewDecoder(resp.Body).Decode(&res)
	if res.Code != 0 {
		return "", fmt.Errorf("Lark auth failed code=%d", res.Code)
	}
	larkCachedToken = res.TenantAccessToken
	larkTokenExpiry = time.Now().Add(time.Duration(res.Expire-60) * time.Second)
	return larkCachedToken, nil
}

var larkStatusLabels = map[string]string{
	"registrator": "Registrator",
	"present":     "Present",
	"demo":        "Demo",
	"site_survey": "Site Survey",
	"quotation":   "Quotation",
	"tor":         "TOR",
	"contract":    "Contract",
	"closed":      "Closed",
	"reject":      "Reject",
}

func getLarkStatusLabel(status string) string {
	if label, ok := larkStatusLabels[status]; ok {
		return label
	}
	return status
}

func buildLarkFields(p models.Project, companyName string) map[string]interface{} {
	return map[string]interface{}{
		"ชื่อหน่วยงาน":   p.AgencyName,
		"ประเภทหน่วยงาน": p.AgencyType,
		"จังหวัด":        p.Region,
		"ผู้ติดต่อ":      p.ContactPerson,
		"โทรศัพท์":       p.ContactPhone,
		"บริษัท Dealer":  companyName,
		"สถานะ":          getLarkStatusLabel(p.Status),
		"Project ID":     p.ID,
		"วันที่สร้าง":    p.CreatedAt.Format("2006-01-02"),
	}
}

func findLarkRecord(token, appToken, tableID, projectID string) (string, error) {
	url := fmt.Sprintf(
		"https://open.larksuite.com/open-apis/bitable/v1/apps/%s/tables/%s/records/search",
		appToken, tableID,
	)
	body, _ := json.Marshal(map[string]interface{}{
		"filter": map[string]interface{}{
			"conjunction": "and",
			"conditions": []map[string]interface{}{
				{"field_name": "Project ID", "operator": "is", "value": []string{projectID}},
			},
		},
	})
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var res struct {
		Data struct {
			Items []struct {
				RecordID string `json:"record_id"`
			} `json:"items"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&res)
	if len(res.Data.Items) > 0 {
		return res.Data.Items[0].RecordID, nil
	}
	return "", nil
}

func createLarkRecord(token, appToken, tableID string, fields map[string]interface{}) error {
	url := fmt.Sprintf(
		"https://open.larksuite.com/open-apis/bitable/v1/apps/%s/tables/%s/records",
		appToken, tableID,
	)
	body, _ := json.Marshal(map[string]interface{}{"fields": fields})
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	log.Printf("[LARK] create: %s\n", rb)
	return nil
}

func updateLarkRecord(token, appToken, tableID, recordID string, fields map[string]interface{}) error {
	url := fmt.Sprintf(
		"https://open.larksuite.com/open-apis/bitable/v1/apps/%s/tables/%s/records/%s",
		appToken, tableID, recordID,
	)
	body, _ := json.Marshal(map[string]interface{}{"fields": fields})
	req, _ := http.NewRequest("PUT", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	log.Printf("[LARK] update: %s\n", rb)
	return nil
}

// SyncProjectToLark upserts a single project record into Lark Bitable.
// Safe to call in a goroutine; returns silently if Lark is unconfigured.
func SyncProjectToLark(p models.Project) {
	appToken := os.Getenv("LARK_BASE_APP_TOKEN")
	tableID  := os.Getenv("LARK_TABLE_ID")
	if appToken == "" || tableID == "" {
		return
	}
	token, err := getLarkAccessToken()
	if err != nil {
		log.Printf("[LARK] auth error: %v\n", err)
		return
	}

	// Look up company name from CompanyID
	companyName := ""
	if p.CompanyID != "" {
		_ = config.DB.QueryRow(context.Background(),
			`SELECT COALESCE(name,'') FROM companies WHERE id = $1::uuid`, p.CompanyID,
		).Scan(&companyName)
	}

	fields   := buildLarkFields(p, companyName)
	recordID, err := findLarkRecord(token, appToken, tableID, p.ID)
	if err != nil {
		log.Printf("[LARK] search error project=%s: %v\n", p.ID, err)
		return
	}

	if recordID == "" {
		if err := createLarkRecord(token, appToken, tableID, fields); err != nil {
			log.Printf("[LARK] create error project=%s: %v\n", p.ID, err)
		} else {
			log.Printf("[LARK] created: %s\n", p.AgencyName)
		}
	} else {
		if err := updateLarkRecord(token, appToken, tableID, recordID, fields); err != nil {
			log.Printf("[LARK] update error project=%s: %v\n", p.ID, err)
		} else {
			log.Printf("[LARK] updated: %s → %s\n", p.AgencyName, p.Status)
		}
	}
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

	rows, err := config.DB.Query(context.Background(),
		`SELECT `+projectCols+` FROM projects ORDER BY created_at DESC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch projects"})
		return
	}
	defer rows.Close()

	var projects []models.Project
	for rows.Next() {
		if p, err := scanProject(rows); err == nil {
			projects = append(projects, p)
		}
	}
	rows.Close()

	go func() {
		for _, p := range projects {
			SyncProjectToLark(p)
			time.Sleep(300 * time.Millisecond)
		}
		log.Printf("[LARK] Bulk sync done: %d projects\n", len(projects))
	}()

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("กำลัง sync %d โครงการไปยัง Lark", len(projects)),
	})
}
