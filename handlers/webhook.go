package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

type WebhookBody struct {
	Events []WebhookEvent `json:"events"`
}

type WebhookEvent struct {
	Type       string        `json:"type"`
	ReplyToken string        `json:"replyToken"`
	Source     WebhookSource `json:"source"`
	Message    WebhookMsg    `json:"message"`
}

type WebhookSource struct {
	Type   string `json:"type"`
	UserID string `json:"userId"`
}

type WebhookMsg struct {
	Type string `json:"type"`
	Text string `json:"text"`
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
		switch event.Type {
		case "message":
			if event.Message.Type == "text" {
				// TODO: handle incoming text
			}
		case "follow":
			// TODO: welcome message
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func verifyLineSignature(body []byte, signature string) bool {
	secret := os.Getenv("LINE_CHANNEL_SECRET")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return expected == signature
}
