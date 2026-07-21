package coord

import (
	"os"
	"strconv"
)

// ConfigFromEnv reads server configuration from environment variables.
func ConfigFromEnv() Config {
	rateLimit := 100
	if v := os.Getenv("MESH_RATE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rateLimit = n
		}
	}
	cfg := Config{
		Addr:      envOrDefault("MESH_ADDR", ":8080"),
		DBPath:    envOrDefault("MESH_DB", "data/gpumesh.db"),
		BaseURL:   envOrDefault("MESH_BASE_URL", "http://localhost:8080"),
		RateLimit: rateLimit,
	}
	return cfg
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
