package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// Lark URL verification + Bitable record change webhook (Lark → App).
// Configure in Lark Developer Console → Events → drive.file.bitable_record_changed_v1
// Request URL: https://vizzel-sales-api.fly.dev/api/v1/webhook/lark

type larkWebhookEnvelope struct {
	Challenge string          `json:"challenge"`
	Token     string          `json:"token"`
	Type      string          `json:"type"`
	Schema    string          `json:"schema"`
	Header    larkEventHeader `json:"header"`
	Event     larkBitableEvent `json:"event"`
}

type larkEventHeader struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Token     string `json:"token"`
	AppID     string `json:"app_id"`
}

type larkBitableEvent struct {
	FileType   string              `json:"file_type"`
	FileToken  string              `json:"file_token"`
	TableID    string              `json:"table_id"`
	ActionList []larkRecordAction  `json:"action_list"`
}

type larkRecordAction struct {
	Action   string `json:"action"`
	RecordID string `json:"record_id"`
}

func larkWebhookEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LARK_WEBHOOK_ENABLED"))) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func verifyLarkEventToken(token string) bool {
	want := strings.TrimSpace(os.Getenv("LARK_EVENT_VERIFY_TOKEN"))
	if want == "" {
		// Allow boot without token only if explicitly disabled for local dev
		return strings.ToLower(os.Getenv("LARK_WEBHOOK_ALLOW_UNVERIFIED")) == "true"
	}
	return token == want
}

func HandleLarkWebhook(c *gin.Context) {
	if !larkWebhookEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "lark webhook disabled"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}

	var env larkWebhookEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	token := strings.TrimSpace(env.Token)
	if token == "" {
		token = strings.TrimSpace(env.Header.Token)
	}
	if !verifyLarkEventToken(token) {
		log.Printf("[LARK] webhook rejected: bad token\n")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	// URL verification (first-time setup in Lark console)
	if env.Type == "url_verification" || env.Challenge != "" {
		c.JSON(http.StatusOK, gin.H{"challenge": env.Challenge})
		return
	}

	eventType := strings.TrimSpace(env.Header.EventType)
	if eventType == "" {
		eventType = strings.TrimSpace(env.Type)
	}

	switch eventType {
	case "drive.file.bitable_record_changed_v1", "bitable.record.changed":
		tableID := env.Event.TableID
		for _, act := range env.Event.ActionList {
			recID := strings.TrimSpace(act.RecordID)
			action := strings.TrimSpace(act.Action)
			if recID == "" {
				continue
			}
			go processLarkBitableRecordChange(tableID, recID, action)
		}
	default:
		log.Printf("[LARK] webhook ignored event_type=%s\n", eventType)
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
