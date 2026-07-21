package coord

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"
)

// --- GET /api/status ---

func (s *Server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	snap := s.registry.Snapshot()
	uptime := time.Since(s.startTime)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"donors_online": snap.DonorsOnline,
		"models_online": snap.ModelsOnline,
		"requests_today": s.requestsToday,
		"tokens_today":   s.tokensToday,
		"uptime":         formatDuration(uptime),
	})
}

// --- GET /api/consumer/stats ---

func (s *Server) handleConsumerStats(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	// Find a consumer key for this user to check rate limit.
	keys, err := s.store.ListKeys(userID)
	var remaining int
	rateLimit := 100
	if err == nil && len(keys) > 0 {
		hash := keys[0].KeyHash
		remaining = s.limiter.Remaining(hash)
	} else {
		remaining = rateLimit
	}

	ds, _ := s.store.GetDonorStats(userID)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"requests_today": ds.TotalRequests,
		"tokens_today":   ds.TotalTokens,
		"rate_limit":     rateLimit,
		"rate_remaining": remaining,
	})
}

// --- GET /api/donor/stats ---

func (s *Server) handleDonorStatsAPI(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	ds, err := s.store.GetDonorStats(userID)
	if err != nil {
		log.Printf("donor stats error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_requests":      ds.TotalRequests,
		"total_tokens":        ds.TotalTokens,
		"total_uptime_seconds": ds.TotalUptimeSec,
		"badge":               BadgeForTokens(ds.TotalTokens),
	})
}

// --- GET /api/donor/status ---

func (s *Server) handleDonorStatus(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	donors := s.registry.DonorsForUser(userID)
	agents := make([]map[string]interface{}, 0, len(donors))
	for _, d := range donors {
		uptime := time.Since(d.ConnectedAt)
		agents = append(agents, map[string]interface{}{
			"provider_id": d.ProviderID,
			"online":      true,
			"models":      d.Models,
			"load":        fmt.Sprintf("%d/%d", d.CurrentLoad, d.MaxConcurrent),
			"uptime":      formatDuration(uptime),
			"description": d.Description,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agents": agents,
	})
}

// --- GET /leaderboard/data ---

func (s *Server) handleLeaderboardData(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	limit := 50

	// In MVP, leaderboard is all-time from donor_stats.
	// Get all active donor connections for online status, plus stats from DB.
	_ = period

	// Build entries from registry (online donors) plus persistent stats.

	type entry struct {
		Rank        int    `json:"rank"`
		GithubLogin string `json:"github_login"`
		AvatarURL   string `json:"avatar_url"`
		Tokens      int64  `json:"tokens"`
		Requests    int64  `json:"requests"`
		Badge       string `json:"badge"`
	}

	// Collect all donors from registry and cross-reference with stats.
	// For MVP we aggregate from the registry session + persistent stats.
	var entries []entry
	seenUsers := map[int64]bool{}

	for _, d := range s.registry.donors {
		if seenUsers[d.UserID] {
			continue
		}
		seenUsers[d.UserID] = true

		ds, _ := s.store.GetDonorStats(d.UserID)
		login := s.getGithubLogin(d.UserID)
		entries = append(entries, entry{
			GithubLogin: login,
			AvatarURL:   fmt.Sprintf("https://github.com/%s.png", login),
			Tokens:      ds.TotalTokens,
			Requests:    ds.TotalRequests,
			Badge:       BadgeForTokens(ds.TotalTokens),
		})
	}

	// Sort by tokens descending.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Tokens > entries[j].Tokens
	})

	// Apply limit.
	if len(entries) > limit {
		entries = entries[:limit]
	}

	// Assign ranks.
	for i := range entries {
		entries[i].Rank = i + 1
	}

	if entries == nil {
		entries = []entry{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"entries": entries,
	})
}

// --- GET /models/data ---

func (s *Server) handleModelsData(w http.ResponseWriter, r *http.Request) {
	snap := s.registry.Snapshot()

	type modelEntry struct {
		ID           string   `json:"id"`
		DonorsOnline int      `json:"donors_online"`
		Load         float64  `json:"load"`
		Tags         []string `json:"tags"`
	}

	var models []modelEntry
	for name, ms := range snap.Models {
		models = append(models, modelEntry{
			ID:           name,
			DonorsOnline: ms.DonorsOnline,
			Load:         ms.Load,
			Tags:         []string{"chat"}, // Default tag for MVP.
		})
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	if models == nil {
		models = []modelEntry{}
	}

	writeJSON(w, http.StatusOK, models)
}

// --- HTMX dashboard fragments ---

func (s *Server) handleDashboardConsumer(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	login := s.getGithubLogin(userID)
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div id="tab-consumer">
<h2>Consumer</h2>
<p>Welcome, %s</p>
<p>Your API keys and usage stats will appear here.</p>
</div>`, login)
}

func (s *Server) handleDashboardDonor(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	donors := s.registry.DonorsForUser(userID)
	w.Header().Set("Content-Type", "text/html")
	if len(donors) == 0 {
		fmt.Fprintf(w, `<div id="tab-donor">
<h2>Donor</h2>
<p>No agents connected. Run gpumesh-provider to share your GPU.</p>
</div>`)
		return
	}
	fmt.Fprintf(w, `<div id="tab-donor">
<h2>Donor</h2>
<p>%d agent(s) online</p>
</div>`, len(donors))
}

// --- Helpers ---

func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// handleModelsDataJSON provides raw JSON for HTMX polling.
func (s *Server) handleModelsDataJSON(w http.ResponseWriter, _ *http.Request) {
	s.handleModelsData(w, nil)
}

// --- Logout handler ---

// Already defined in oauth.go
var _ = json.Marshal // keep json import
