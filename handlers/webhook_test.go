package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func signLineBody(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestHandleWebhookPostbackCompanyList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "test-line-secret"
	os.Setenv("LINE_CHANNEL_SECRET", secret)
	defer os.Unsetenv("LINE_CHANNEL_SECRET")

	body, _ := json.Marshal(map[string]interface{}{
		"events": []map[string]interface{}{
			{
				"type":       "postback",
				"replyToken": "reply-token-test",
				"postback":   map[string]string{"data": "action=company_list"},
			},
		},
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/webhook", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Line-Signature", signLineBody(body, secret))

	HandleWebhook(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
}

func TestHandleWebhookInvalidSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	os.Setenv("LINE_CHANNEL_SECRET", "test-line-secret")
	defer os.Unsetenv("LINE_CHANNEL_SECRET")

	body := []byte(`{"events":[]}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/webhook", bytes.NewReader(body))
	c.Request.Header.Set("X-Line-Signature", "bad")

	HandleWebhook(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", w.Code)
	}
}
