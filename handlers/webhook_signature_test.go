package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"testing"
)

// BUG-09: verifyLineSignature compares HMACs with `==` instead of
// hmac.Equal/subtle.ConstantTimeCompare (a timing side-channel, not
// practically assertable from a unit test), and when LINE_CHANNEL_SECRET is
// unset it still computes and compares an HMAC keyed with an empty string —
// so an attacker who knows the secret is unconfigured can precompute a valid
// signature. verifyLineSignature should reject all requests when the secret
// is not configured, regardless of the supplied signature.
// See BUG_REPORT.md BUG-09.
func TestVerifyLineSignature_RejectsWhenSecretUnconfigured(t *testing.T) {
	os.Unsetenv("LINE_CHANNEL_SECRET")

	body := []byte(`{"events":[]}`)
	mac := hmac.New(sha256.New, []byte("")) // what verifyLineSignature computes with an empty secret
	mac.Write(body)
	forged := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if verifyLineSignature(body, forged) {
		t.Fatalf("verifyLineSignature accepted a signature computed against an empty key while " +
			"LINE_CHANNEL_SECRET is unset — it must reject all requests when the secret isn't " +
			"configured, not silently verify against an empty key (BUG-09)")
	}
}

func TestVerifyLineSignature_AcceptsCorrectSignature(t *testing.T) {
	os.Setenv("LINE_CHANNEL_SECRET", "a-real-secret")
	defer os.Unsetenv("LINE_CHANNEL_SECRET")

	body := []byte(`{"events":[]}`)
	mac := hmac.New(sha256.New, []byte("a-real-secret"))
	mac.Write(body)
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if !verifyLineSignature(body, sig) {
		t.Fatalf("verifyLineSignature rejected a correctly-signed request")
	}
}
