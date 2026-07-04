package config

import (
	"os"
	"strings"
)

// DefaultCORSOrigins are allowed browser origins for the Sales API.
var DefaultCORSOrigins = []string{
	"https://staging-sale.vizzeltrack.com",
	"https://sale.vizzeltrack.com",
	"https://vizzelintel.github.io", // remove after cutover
}

// AllowedCORSOrigins parses CORS_ORIGINS (comma-separated) or returns defaults.
func AllowedCORSOrigins() map[string]bool {
	raw := strings.TrimSpace(os.Getenv("CORS_ORIGINS"))
	if raw == "" {
		return sliceToSet(DefaultCORSOrigins)
	}
	parts := strings.Split(raw, ",")
	out := make(map[string]bool, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out[p] = true
		}
	}
	return out
}

func sliceToSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[item] = true
	}
	return out
}
