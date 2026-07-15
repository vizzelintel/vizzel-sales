package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const lineReplyURL = "https://api.line.me/v2/bot/message/reply"
const lineOAuthURL = "https://api.line.me/v2/oauth/accessToken"

var (
	lineTokenMu    sync.Mutex
	lineTokenCache string
	lineTokenUntil time.Time
)

// lineChannelID returns Messaging API channel ID from env or LIFF_ID prefix.
func lineChannelID() string {
	if id := strings.TrimSpace(os.Getenv("LINE_CHANNEL_ID")); id != "" {
		return id
	}
	liff := strings.TrimSpace(os.Getenv("LIFF_ID"))
	if i := strings.Index(liff, "-"); i > 0 {
		return liff[:i]
	}
	return liff
}

func lineChannelSecret() string {
	return strings.TrimSpace(os.Getenv("LINE_CHANNEL_SECRET"))
}

func getLineChannelAccessToken() (string, error) {
	if t := strings.TrimSpace(os.Getenv("LINE_CHANNEL_ACCESS_TOKEN")); t != "" {
		return t, nil
	}

	lineTokenMu.Lock()
	defer lineTokenMu.Unlock()

	if lineTokenCache != "" && time.Now().Before(lineTokenUntil.Add(-time.Minute)) {
		return lineTokenCache, nil
	}

	channelID := lineChannelID()
	secret := lineChannelSecret()
	if channelID == "" || secret == "" {
		return "", fmt.Errorf("LINE_CHANNEL_ACCESS_TOKEN unset and cannot issue token (missing channel id/secret)")
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", channelID)
	form.Set("client_secret", secret)

	req, err := http.NewRequest(http.MethodPost, lineOAuthURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LINE OAuth %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.AccessToken == "" {
		return "", fmt.Errorf("LINE OAuth: empty access_token")
	}

	lineTokenCache = parsed.AccessToken
	expires := parsed.ExpiresIn
	if expires <= 0 {
		expires = 3600
	}
	lineTokenUntil = time.Now().Add(time.Duration(expires) * time.Second)
	log.Printf("LINE: issued channel access token (expires in %ds)", expires)
	return lineTokenCache, nil
}

func lineReply(replyToken string, messages []map[string]interface{}) error {
	token, err := getLineChannelAccessToken()
	if err != nil {
		return err
	}

	payload, err := json.Marshal(map[string]interface{}{
		"replyToken": replyToken,
		"messages":   messages,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, lineReplyURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("LINE Reply API %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
