package coord

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// ConfigFromEnv reads server configuration from environment variables.
// Loads .env file first if present (env vars take precedence).
func ConfigFromEnv() Config {
	loadDotEnv(".env")

	rateLimit := 100
	if v := os.Getenv("MESH_RATE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rateLimit = n
		}
	}
	affinityTTL := 120
	if v := os.Getenv("MESH_AFFINITY_TTL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			affinityTTL = n
		}
	}
	cfg := Config{
		Addr:        envOrDefault("MESH_ADDR", ":8080"),
		DBPath:      envOrDefault("MESH_DB", "data/gpumesh.db"),
		BaseURL:     envOrDefault("MESH_BASE_URL", "http://localhost:8080"),
		RateLimit:   rateLimit,
		AffinityTTL: affinityTTL,
	}
	return cfg
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// loadDotEnv reads KEY=VALUE pairs from path and sets them via os.Setenv
// if not already set in the environment. Skips comments and empty lines.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Support KEY=VALUE and export KEY=VALUE
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		// Remove surrounding quotes.
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		// Don't override existing env vars.
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}
