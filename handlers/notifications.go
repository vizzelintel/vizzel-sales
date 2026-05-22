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

	"vizzel-backend/config"
	"vizzel-backend/models"
)

func notificationsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("NOTIFY_ENABLED"))) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func larkNotifyEnabled() bool {
	if !notificationsEnabled() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LARK_NOTIFY_ENABLED"))) {
	case "0", "false", "no", "off":
		return false
	default:
		return strings.TrimSpace(os.Getenv("LARK_NOTIFY_CHAT_ID")) != ""
	}
}

func loadNotifyEmails(ctx context.Context) []string {
	seen := map[string]struct{}{}
	var out []string

	rows, err := config.DB.Query(ctx,
		`SELECT DISTINCT LOWER(TRIM(email))
		 FROM users
		 WHERE email_verified_at IS NOT NULL
		   AND TRIM(COALESCE(email,'')) <> ''`,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var e string
			if rows.Scan(&e) == nil && e != "" {
				if _, ok := seen[e]; !ok {
					seen[e] = struct{}{}
					out = append(out, e)
				}
			}
		}
	}

	for _, raw := range strings.Split(os.Getenv("NOTIFY_EXTRA_EMAILS"), ",") {
		e := strings.ToLower(strings.TrimSpace(raw))
		if e == "" {
			continue
		}
		if _, ok := seen[e]; !ok {
			seen[e] = struct{}{}
			out = append(out, e)
		}
	}
	return out
}

func sendLarkChatText(text string) error {
	chatID := strings.TrimSpace(os.Getenv("LARK_NOTIFY_CHAT_ID"))
	if chatID == "" {
		return fmt.Errorf("LARK_NOTIFY_CHAT_ID not set")
	}
	token, err := getLarkAccessToken()
	if err != nil {
		return err
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	body, _ := json.Marshal(map[string]interface{}{
		"receive_id": chatID,
		"msg_type":   "text",
		"content":    string(content),
	})
	url := larkBaseURL() + "/open-apis/im/v1/messages?receive_id_type=chat_id"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := larkHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	var res larkAPIResp
	_ = json.Unmarshal(rb, &res)
	if res.Code != 0 {
		return fmt.Errorf("lark im code=%d msg=%s", res.Code, res.Msg)
	}
	return nil
}

func larkBitableRecordURL(appToken, tableID, recordID string) string {
	if u := strings.TrimSpace(os.Getenv("LARK_BASE_VIEW_URL")); u != "" {
		return strings.ReplaceAll(u, "{record_id}", recordID)
	}
	if appToken == "" || tableID == "" || recordID == "" {
		return ""
	}
	return fmt.Sprintf("https://www.larksuite.com/base/%s?table=%s&record=%s", appToken, tableID, recordID)
}

// NotifyNewProjectLark posts a short message to the configured Lark group chat.
func NotifyNewProjectLark(p models.Project, companyName string) {
	if !larkNotifyEnabled() {
		return
	}
	recordID := strings.TrimSpace(p.LarkRecordID)
	if recordID == "" {
		recordID = loadLarkRecordID(p.ID)
	}
	link := ""
	if token, err := getLarkAccessToken(); err == nil {
		if cfg, err := resolveLarkBitableConfig(token); err == nil {
			link = larkBitableRecordURL(cfg.AppToken, cfg.TableID, recordID)
		}
	}
	dealer := strings.TrimSpace(companyName)
	if dealer == "" {
		dealer = "—"
	}
	lines := []string{
		"📋 โครงการใหม่ใน Vizzel Sales",
		fmt.Sprintf("หน่วยงาน: %s", p.AgencyName),
		fmt.Sprintf("จังหวัด: %s", strings.TrimSpace(p.Region)),
		fmt.Sprintf("Dealer: %s", dealer),
		fmt.Sprintf("สถานะ: %s", getLarkStatusLabel(p.Status)),
		fmt.Sprintf("Project ID: %s", p.ID),
	}
	if link != "" {
		lines = append(lines, "เปิดใน Lark: "+link)
	}
	if err := sendLarkChatText(strings.Join(lines, "\n")); err != nil {
		log.Printf("[NOTIFY] lark new project: %v\n", err)
	} else {
		log.Printf("[NOTIFY] lark new project sent: %s\n", p.AgencyName)
	}
}

func sendCalendarInviteToAll(emails []string, agencyName, statusLabel, note string, start time.Time, projectID, meetLink string) (sent int, lastErr error) {
	if len(emails) == 0 {
		return 0, fmt.Errorf("ไม่มีอีเมลผู้รับแจ้งเตือน")
	}
	loc := time.FixedZone("ICT", 7*3600)
	end := start.Add(time.Hour)
	subject := fmt.Sprintf("[Vizzel] นัดหมาย%s - %s", statusLabel, agencyName)
	timeStr := start.In(loc).Format("02/01/2006 15:04")
	textBody := fmt.Sprintf(
		"มีการนัดหมาย %s สำหรับโครงการ %s\nเวลา: %s\n\nหมายเหตุ: %s\n",
		statusLabel, agencyName, timeStr, note,
	)
	if meetLink != "" {
		textBody += "\nGoogle Meet: " + meetLink + "\n"
	}
	textBody += "\nเปิดไฟล์แนบ invite.ics เพื่อนำเข้าปฏิทิน (Google/Outlook/Apple) ได้ทันที"

	uidBase := fmt.Sprintf("%s-%s-%d", projectID, strings.ToLower(statusLabel), start.Unix())
	location := "Thailand"
	if meetLink != "" {
		location = meetLink
	}
	desc := textBody

	for i, email := range emails {
		uid := fmt.Sprintf("%s-%d@vizzel.sales", uidBase, i)
		ics := buildICSInvite(uid, subject, desc, location, start, end)
		if err := sendSMTPMailWithICS(email, subject, textBody, ics, "invite.ics"); err != nil {
			lastErr = err
			log.Printf("[NOTIFY] calendar mail to %s: %v\n", email, err)
			continue
		}
		sent++
	}
	return sent, lastErr
}
