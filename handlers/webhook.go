package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

type WebhookBody struct {
	Events []WebhookEvent `json:"events"`
}

type WebhookEvent struct {
	Type       string          `json:"type"`
	ReplyToken string          `json:"replyToken"`
	Source     WebhookSource   `json:"source"`
	Message    WebhookMsg      `json:"message"`
	Postback   WebhookPostback `json:"postback"`
}

type WebhookSource struct {
	Type   string `json:"type"`
	UserID string `json:"userId"`
}

type WebhookMsg struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type WebhookPostback struct {
	Data   string `json:"data"`
	Params struct {
		Datetime string `json:"datetime"`
	} `json:"params"`
}

func HandleWebhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}

	sig := c.GetHeader("X-Line-Signature")
	if !verifyLineSignature(body, sig) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
		return
	}

	var wb WebhookBody
	if err := json.Unmarshal(body, &wb); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	for _, event := range wb.Events {
		handleLineWebhookEvent(event)
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func handleLineWebhookEvent(event WebhookEvent) {
	switch event.Type {
	case "postback":
		if event.ReplyToken == "" {
			return
		}
		if isCompanyListAction(event.Postback.Data) {
			go replyCompanyListFlex(event.ReplyToken)
		}
	case "message":
		if event.ReplyToken == "" || event.Message.Type != "text" {
			return
		}
		text := strings.TrimSpace(event.Message.Text)
		if text == "รายชื่อตัวแทนจำหน่าย" || text == "รายชื่อบริษัท" {
			go replyCompanyListFlex(event.ReplyToken)
		}
	case "follow":
		// optional welcome message
	}
}

func verifyLineSignature(body []byte, signature string) bool {
	secret := os.Getenv("LINE_CHANNEL_SECRET")
	if secret == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		log.Printf("LINE webhook: invalid signature")
		return false
	}
	return true
}
