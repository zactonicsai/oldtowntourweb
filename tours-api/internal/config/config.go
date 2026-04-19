// Package config loads runtime settings from environment variables.
//
// Supported vars:
//
//	PORT              HTTP listen port.               Default: 8080
//	API_SHARED_KEY    Required; all /api/* requests   (no default, required)
//	                  must send X-API-Key: <value>.
//	DATA_DIR          Root for local JSON + blobs.    Default: ./data
//	ALLOWED_ORIGINS   Comma-separated CORS origins,   Default: *
//	                  or "*" to allow any origin.
//	MAX_UPLOAD_MB     Max upload size in megabytes.   Default: 100
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds every piece of runtime configuration the server needs.
type Config struct {
	Port           string
	SharedKey      string
	DataDir        string
	AllowedOrigins []string
	MaxUploadBytes int64
}

// FromEnv reads configuration from the process environment and
// returns an error if any required variable is missing / invalid.
func FromEnv() (*Config, error) {
	cfg := &Config{
		Port:      getEnv("PORT", "8080"),
		SharedKey: os.Getenv("API_SHARED_KEY"),
		DataDir:   getEnv("DATA_DIR", "./data"),
	}

	if strings.TrimSpace(cfg.SharedKey) == "" {
		return nil, errors.New("API_SHARED_KEY is required")
	}

	// Validate port is numeric and in range.
	if p, err := strconv.Atoi(cfg.Port); err != nil || p < 1 || p > 65535 {
		return nil, fmt.Errorf("PORT %q is not a valid port number", cfg.Port)
	}

	origins := getEnv("ALLOWED_ORIGINS", "*")
	for _, o := range strings.Split(origins, ",") {
		if trimmed := strings.TrimSpace(o); trimmed != "" {
			cfg.AllowedOrigins = append(cfg.AllowedOrigins, trimmed)
		}
	}

	maxMB, err := strconv.Atoi(getEnv("MAX_UPLOAD_MB", "100"))
	if err != nil || maxMB <= 0 {
		return nil, fmt.Errorf("MAX_UPLOAD_MB %q is not a positive integer", getEnv("MAX_UPLOAD_MB", ""))
	}
	cfg.MaxUploadBytes = int64(maxMB) * 1024 * 1024

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
