package coord

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// ConfigFromEnv reads server configuration from environment variables.
func ConfigFromEnv() Config {
	loadDotEnv(".env")

	rateLimit := 100
	if v := os.Getenv("MESH_RATE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rateLimit = n
		}
	}
	inviteTTL := 7
	if v := os.Getenv("MESH_INVITE_TTL_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			inviteTTL = n
		}
	}
	inviteMaxUses := 1
	if v := os.Getenv("MESH_INVITE_MAX_USES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			inviteMaxUses = n
		}
	}
	pinAttemptLimit := 10
	if v := os.Getenv("MESH_PIN_ATTEMPT_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pinAttemptLimit = n
		}
	}
	return Config{
		Addr:            envOrDefault("MESH_ADDR", ":8080"),
		DBPath:          envOrDefault("MESH_DB", "data/gpumesh.db"),
		BaseURL:         envOrDefault("MESH_BASE_URL", "http://localhost:8080"),
		RateLimit:       rateLimit,
		InviteTTLDays:   inviteTTL,
		InviteMaxUses:   inviteMaxUses,
		PinAttemptLimit: pinAttemptLimit,
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

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
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}
