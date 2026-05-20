package handlers

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type serviceAccountKey struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// CreateCalendarEvent creates a 1-hour Google Calendar event via the REST API
// using a Service Account credential from GOOGLE_CALENDAR_CREDENTIALS_JSON.
// attendees is a slice of email addresses to invite; Google sends each one an
// email notification automatically (sendUpdates=all).
// Returns the created event ID, or an error (caller decides whether to surface it).
func CreateCalendarEvent(title, description, datetimeRFC3339 string, attendees []string) (string, error) {
	credsJSON := os.Getenv("GOOGLE_CALENDAR_CREDENTIALS_JSON")
	if credsJSON == "" {
		return "", fmt.Errorf("GOOGLE_CALENDAR_CREDENTIALS_JSON not configured")
	}
	calendarID := os.Getenv("GOOGLE_CALENDAR_ID")
	if calendarID == "" {
		calendarID = "primary"
	}

	var sa serviceAccountKey
	if err := json.Unmarshal([]byte(credsJSON), &sa); err != nil {
		return "", fmt.Errorf("parse credentials JSON: %w", err)
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}

	key, err := parseRSAKey(sa.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}

	accessToken, err := googleAccessToken(sa.ClientEmail, sa.TokenURI, key)
	if err != nil {
		return "", fmt.Errorf("get access token: %w", err)
	}

	start, err := time.Parse(time.RFC3339, datetimeRFC3339)
	if err != nil {
		return "", fmt.Errorf("parse datetime %q: %w", datetimeRFC3339, err)
	}

	eventID, err := insertEvent(accessToken, calendarID, title, description, start, start.Add(time.Hour), attendees)
	if err != nil {
		return "", fmt.Errorf("insert event: %w", err)
	}
	return eventID, nil
}

// parseRSAKey decodes a PEM private key (PKCS#8 or PKCS#1).
// Env vars often store literal "\n" — this handles that too.
func parseRSAKey(pemStr string) (*rsa.PrivateKey, error) {
	pemStr = strings.ReplaceAll(pemStr, `\n`, "\n")
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	switch block.Type {
	case "PRIVATE KEY": // PKCS#8 (Google default)
		raw, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		k, ok := raw.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("not an RSA key")
		}
		return k, nil
	case "RSA PRIVATE KEY": // PKCS#1
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported PEM type: %s", block.Type)
	}
}

// googleAccessToken exchanges a signed JWT for a Google OAuth2 access token.
func googleAccessToken(clientEmail, tokenURI string, key *rsa.PrivateKey) (string, error) {
	now := time.Now()

	hdr := b64j(map[string]string{"alg": "RS256", "typ": "JWT"})
	cls := b64j(map[string]any{
		"iss":   clientEmail,
		"scope": "https://www.googleapis.com/auth/calendar",
		"aud":   tokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})

	sigInput := hdr + "." + cls
	h := sha256.New()
	h.Write([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h.Sum(nil))
	if err != nil {
		return "", err
	}
	jwtStr := sigInput + "." + base64.RawURLEncoding.EncodeToString(sig)

	resp, err := http.PostForm(tokenURI, url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {jwtStr},
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	if tok.Error != "" {
		return "", fmt.Errorf("token exchange: %s – %s", tok.Error, tok.ErrorDesc)
	}
	return tok.AccessToken, nil
}

// insertEvent POSTs a Calendar event and returns the event ID.
// When attendees are provided, sendUpdates=all causes Google to email each one.
func insertEvent(accessToken, calendarID, title, description string, start, end time.Time, attendees []string) (string, error) {
	event := map[string]any{
		"summary":     title,
		"description": description,
		"start":       map[string]string{"dateTime": start.Format(time.RFC3339), "timeZone": "Asia/Bangkok"},
		"end":         map[string]string{"dateTime": end.Format(time.RFC3339), "timeZone": "Asia/Bangkok"},
	}
	if len(attendees) > 0 {
		att := make([]map[string]string, 0, len(attendees))
		for _, email := range attendees {
			if email != "" {
				att = append(att, map[string]string{"email": email})
			}
		}
		if len(att) > 0 {
			event["attendees"] = att
		}
	}
	payload, _ := json.Marshal(event)

	apiURL := fmt.Sprintf(
		"https://www.googleapis.com/calendar/v3/calendars/%s/events?sendUpdates=all",
		url.PathEscape(calendarID),
	)
	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var ev struct {
		ID    string `json:"id"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return "", fmt.Errorf("parse event response: %w", err)
	}
	if ev.Error != nil {
		return "", fmt.Errorf("calendar API: %s", ev.Error.Message)
	}
	return ev.ID, nil
}

// b64j JSON-encodes v and base64url-encodes the result (for JWT segments).
func b64j(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}
