package handlers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// Lark URL verification + Bitable record change webhook (Lark → App).
// Request URL: https://vizzel-sales-api.fly.dev/api/v1/webhook/lark

type larkWebhookEnvelope struct {
	Challenge string           `json:"challenge"`
	Token     string           `json:"token"`
	Type      string           `json:"type"`
	Encrypt   string           `json:"encrypt"`
	Schema    string           `json:"schema"`
	Header    larkEventHeader  `json:"header"`
	Event     json.RawMessage  `json:"event"`
}

type larkEventHeader struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Token     string `json:"token"`
	AppID     string `json:"app_id"`
}

type larkBitableEvent struct {
	FileType   string             `json:"file_type"`
	FileToken  string             `json:"file_token"`
	TableID    string             `json:"table_id"`
	Challenge  string             `json:"challenge"`
	ActionList []larkRecordAction `json:"action_list"`
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

func larkEventVerifyToken() string {
	return strings.TrimSpace(os.Getenv("LARK_EVENT_VERIFY_TOKEN"))
}

func verifyLarkEventToken(token string) bool {
	want := larkEventVerifyToken()
	if want == "" {
		return strings.ToLower(os.Getenv("LARK_WEBHOOK_ALLOW_UNVERIFIED")) == "true"
	}
	return strings.TrimSpace(token) == want
}

func decryptLarkPayload(encryptKey, cipherText string) ([]byte, error) {
	buf, err := base64.StdEncoding.DecodeString(cipherText)
	if err != nil {
		return nil, err
	}
	if len(buf) < aes.BlockSize {
		return nil, fmt.Errorf("cipher too short")
	}
	iv := buf[:aes.BlockSize]
	data := buf[aes.BlockSize:]
	key := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid cipher length")
	}
	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(data))
	mode.CryptBlocks(plain, data)
	n := int(plain[len(plain)-1])
	if n <= 0 || n > aes.BlockSize || n > len(plain) {
		return plain, nil
	}
	return plain[:len(plain)-n], nil
}

func parseLarkWebhookBody(body []byte) (larkWebhookEnvelope, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return larkWebhookEnvelope{}, err
	}
	if enc, ok := top["encrypt"]; ok {
		var cipher string
		_ = json.Unmarshal(enc, &cipher)
		key := strings.TrimSpace(os.Getenv("LARK_ENCRYPT_KEY"))
		if key == "" {
			return larkWebhookEnvelope{}, fmt.Errorf("encrypt key configured in Lark but LARK_ENCRYPT_KEY not set on server")
		}
		plain, err := decryptLarkPayload(key, cipher)
		if err != nil {
			return larkWebhookEnvelope{}, fmt.Errorf("decrypt: %w", err)
		}
		body = plain
	}
	var env larkWebhookEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return larkWebhookEnvelope{}, err
	}
	// schema 2.0: challenge may live under event
	if env.Challenge == "" && len(env.Event) > 0 {
		var ev larkBitableEvent
		if json.Unmarshal(env.Event, &ev) == nil && ev.Challenge != "" {
			env.Challenge = ev.Challenge
		}
	}
	return env, nil
}

func isLarkURLVerification(env larkWebhookEnvelope) bool {
	if env.Challenge != "" && (env.Type == "url_verification" || env.Type == "") {
		return true
	}
	if env.Type == "url_verification" {
		return true
	}
	if strings.EqualFold(env.Header.EventType, "url_verification") {
		return true
	}
	return false
}

func respondLarkChallenge(c *gin.Context, challenge string) {
	c.JSON(http.StatusOK, gin.H{"challenge": challenge})
}

// extractBitableRecordActions walks webhook JSON when action_list shape differs (schema 2.0 variants).
func extractBitableRecordActions(raw json.RawMessage) []larkRecordAction {
	var out []larkRecordAction
	seen := make(map[string]bool)
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch x := v.(type) {
		case map[string]interface{}:
			rid, _ := x["record_id"].(string)
			rid = strings.TrimSpace(rid)
			act, _ := x["action"].(string)
			if rid != "" && !seen[rid] {
				seen[rid] = true
				out = append(out, larkRecordAction{RecordID: rid, Action: strings.TrimSpace(act)})
			}
			for _, key := range []string{"action_list", "actions", "records"} {
				if arr, ok := x[key].([]interface{}); ok {
					for _, it := range arr {
						walk(it)
					}
				}
			}
			for _, val := range x {
				walk(val)
			}
		case []interface{}:
			for _, it := range x {
				walk(it)
			}
		}
	}
	var root interface{}
	if json.Unmarshal(raw, &root) == nil {
		walk(root)
	}
	return out
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
	log.Printf("[LARK] webhook POST bytes=%d ua=%q\n", len(body), c.Request.UserAgent())

	env, err := parseLarkWebhookBody(body)
	if err != nil {
		log.Printf("[LARK] webhook parse: %v\n", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token := strings.TrimSpace(env.Token)
	if token == "" {
		token = strings.TrimSpace(env.Header.Token)
	}

	// URL verification must respond within ~1s with {"challenge": "..."}
	if isLarkURLVerification(env) {
		if env.Challenge == "" {
			log.Printf("[LARK] url_verification missing challenge\n")
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing challenge"})
			return
		}
		if !verifyLarkEventToken(token) {
			log.Printf("[LARK] url_verification token mismatch (set Verification Token in Lark = LARK_EVENT_VERIFY_TOKEN on Fly)\n")
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid token — ใส่ Verification Token ใน Lark ให้ตรงกับ LARK_EVENT_VERIFY_TOKEN บน Fly",
			})
			return
		}
		log.Printf("[LARK] url_verification ok\n")
		respondLarkChallenge(c, env.Challenge)
		return
	}

	if !verifyLarkEventToken(token) {
		log.Printf("[LARK] webhook rejected: bad token\n")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	eventType := strings.TrimSpace(env.Header.EventType)
	if eventType == "" {
		eventType = strings.TrimSpace(env.Type)
	}

	switch eventType {
	case "drive.file.bitable_record_changed_v1", "drive.file.bitable_record_changed_v2",
		"bitable.record.changed", "base.record.changed":
		var ev larkBitableEvent
		if len(env.Event) > 0 {
			_ = json.Unmarshal(env.Event, &ev)
		}
		tableID := ev.TableID
		actions := ev.ActionList
		if len(actions) == 0 && len(env.Event) > 0 {
			actions = extractBitableRecordActions(env.Event)
		}
		log.Printf("[LARK] webhook bitable event_type=%s table=%s actions=%d\n",
			eventType, tableID, len(actions))
		for _, act := range actions {
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
