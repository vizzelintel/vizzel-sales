package config

import (
	"log"
	"os"
	"strings"
)

// JWTSecret signs and verifies API tokens. Must be set in production.
// Falls back to LINE_CHANNEL_SECRET only for backward compatibility during cutover.
func JWTSecret() string {
	if s := strings.TrimSpace(os.Getenv("JWT_SECRET")); s != "" {
		return s
	}
	if s := strings.TrimSpace(os.Getenv("LINE_CHANNEL_SECRET")); s != "" {
		log.Println("WARN: JWT_SECRET unset — using LINE_CHANNEL_SECRET (set JWT_SECRET in production)")
		return s
	}
	return ""
}

// EmailOTPSecret hashes email OTP codes.
func EmailOTPSecret() string {
	if s := strings.TrimSpace(os.Getenv("EMAIL_OTP_SECRET")); s != "" {
		return s
	}
	return JWTSecret()
}
