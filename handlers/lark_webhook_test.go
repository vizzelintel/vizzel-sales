package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHandleLarkWebhookURLVerificationSchema2(t *testing.T) {
	gin.SetMode(gin.TestMode)
	os.Setenv("LARK_EVENT_VERIFY_TOKEN", "test-token")
	os.Setenv("LARK_WEBHOOK_ENABLED", "true")
	defer os.Unsetenv("LARK_EVENT_VERIFY_TOKEN")

	body, _ := json.Marshal(map[string]interface{}{
		"schema": "2.0",
		"header": map[string]string{
			"event_type": "url_verification",
			"token":      "test-token",
		},
		"event": map[string]string{"challenge": "ch-v2"},
	})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/webhook/lark", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	HandleLarkWebhook(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	var res map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["challenge"] != "ch-v2" {
		t.Fatalf("got %v", res)
	}
}

func TestHandleLarkWebhookURLVerification(t *testing.T) {
	gin.SetMode(gin.TestMode)
	os.Setenv("LARK_EVENT_VERIFY_TOKEN", "test-token")
	os.Setenv("LARK_WEBHOOK_ENABLED", "true")
	defer os.Unsetenv("LARK_EVENT_VERIFY_TOKEN")

	body, _ := json.Marshal(map[string]string{
		"type":      "url_verification",
		"token":     "test-token",
		"challenge": "challenge-xyz",
	})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/webhook/lark", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	HandleLarkWebhook(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	var res map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["challenge"] != "challenge-xyz" {
		t.Fatalf("challenge=%v", res)
	}
}
