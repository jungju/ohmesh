package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr               string
	DatabasePath       string
	Environment        string
	SessionSecret      string
	SessionCookieName  string
	SessionTTL         time.Duration
	CookieSecure       bool
	AllowedOrigins     []string
	GitHubClientID     string
	GitHubClientSecret string
	GoogleClientID     string
	GoogleClientSecret string
}

func Load() Config {
	return Config{
		Addr:               envString("OHMESH_ADDR", ":8080"),
		DatabasePath:       envString("OHMESH_DATABASE_PATH", "ohmesh.db"),
		Environment:        envString("OHMESH_ENV", "development"),
		SessionSecret:      envString("OHMESH_SESSION_SECRET", "dev-secret-change-me"),
		SessionCookieName:  envString("OHMESH_SESSION_COOKIE", "ohmesh_session"),
		SessionTTL:         envDuration("OHMESH_SESSION_TTL", 30*24*time.Hour),
		CookieSecure:       envBool("OHMESH_COOKIE_SECURE", false),
		AllowedOrigins:     envCSV("OHMESH_ALLOWED_ORIGINS"),
		GitHubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	}
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envCSV(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}
