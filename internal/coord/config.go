package coord

import "os"

// ConfigFromEnv reads server configuration from environment variables.
func ConfigFromEnv() Config {
	cfg := Config{
		Addr:      envOrDefault("MESH_ADDR", ":8080"),
		DBPath:    envOrDefault("MESH_DB", "data/gpumesh.db"),
		BaseURL:   envOrDefault("MESH_BASE_URL", "http://localhost:8080"),
		RateLimit: 100,
	}
	if v := os.Getenv("MESH_RATE_LIMIT"); v != "" {
		// Parse would require strconv; keep default for now.
	}
	return cfg
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
