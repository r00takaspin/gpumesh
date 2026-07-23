package coord

import (
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

	// Consumer stats from rate limiter (per-user, aggregated across their keys).
	keys, _ := s.store.ListKeys(userID)
	var remaining int
	rateLimit := s.limiter.Burst()
	if len(keys) > 0 {
		remaining = s.limiter.Remaining(keys[0].KeyHash)
	} else {
		remaining = rateLimit
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"requests_today": rateLimit - remaining,
		"tokens_today":   int64(0), // per-consumer token tracking not in MVP
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

	allStats, err := s.store.ListAllDonorStats()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load leaderboard"})
		return
	}

	seenUsers := map[int64]bool{}
	for _, ds := range allStats {
		if seenUsers[ds.UserID] {
			continue
		}
		seenUsers[ds.UserID] = true

		login := s.getGithubLogin(ds.UserID)
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


// --- GET /leaderboard/page ---

func (s *Server) handleLeaderboardFragment(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	limit := 50
	_ = period

	var entries []LeaderboardEntry
	seenUsers := map[int64]bool{}

	for _, d := range s.registry.donors {
		if seenUsers[d.UserID] {
			continue
		}
		seenUsers[d.UserID] = true

		ds, _ := s.store.GetDonorStats(d.UserID)
		login := s.getGithubLogin(d.UserID)
		entries = append(entries, LeaderboardEntry{
			GithubLogin: login,
			Tokens:      ds.TotalTokens,
			Requests:    ds.TotalRequests,
			Badge:       BadgeForTokens(ds.TotalTokens),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Tokens > entries[j].Tokens
	})

	if len(entries) > limit {
		entries = entries[:limit]
	}

	for i := range entries {
		entries[i].Rank = i + 1
	}

	if entries == nil {
		entries = []LeaderboardEntry{}
	}

	renderTemplate(w, "leaderboard-fragment.html", map[string]interface{}{
		"Entries": entries,
	})
}

// handleDashboardCreateKey creates a key and returns the consumer tab HTML with modal.
func (s *Server) handleDashboardCreateKey(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "consumer"
	}

	rawKey, _, err := s.store.CreateKey(userID, scope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create key")
		return
	}

	// Build the same data as handleDashboardConsumer.
	keys, _ := s.store.ListKeys(userID)
	rateLimit := s.limiter.Burst()
	remaining := rateLimit
	if len(keys) > 0 {
		remaining = s.limiter.Remaining(keys[0].KeyHash)
	}
	requestsToday := rateLimit - remaining

	apiKey := "inf_xxxxxxxx..."
	if len(keys) > 0 {
		apiKey = keys[0].KeyPrefix + "..."
	}

	pct := 0
	if rateLimit > 0 {
		pct = requestsToday * 100 / rateLimit
	}

	type keyView struct {
		ID        int64
		Prefix    string
		Scope     string
		CreatedAt string
	}
	kv := make([]keyView, len(keys))
	for i, k := range keys {
		kv[i] = keyView{
			ID:        k.ID,
			Prefix:    k.KeyPrefix,
			Scope:     k.Scope,
			CreatedAt: k.CreatedAt.Format("2006-01-02"),
		}
	}

	renderTemplate(w, "dashboard-new-key.html", map[string]interface{}{
		"NewKey":         rawKey,
		"APIKey":         apiKey,
		"Keys":           kv,
		"RateLimit":      rateLimit,
		"RequestsToday":  requestsToday,
		"TokensToday":    int64(0),
		"PercentUsed":    pct,
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
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	keys, _ := s.store.ListKeys(userID)
	rateLimit := s.limiter.Burst()
	remaining := rateLimit
	if len(keys) > 0 {
		remaining = s.limiter.Remaining(keys[0].KeyHash)
	}
	requestsToday := rateLimit - remaining

	apiKey := "inf_xxxxxxxx..."
	if len(keys) > 0 {
		apiKey = keys[0].KeyPrefix + "..."
	}

	pct := 0
	if rateLimit > 0 {
		pct = requestsToday * 100 / rateLimit
	}

	type keyView struct {
		ID        int64
		Prefix    string
		Scope     string
		CreatedAt string
	}
	kv := make([]keyView, len(keys))
	for i, k := range keys {
		kv[i] = keyView{
			ID:        k.ID,
			Prefix:    k.KeyPrefix,
			Scope:     k.Scope,
			CreatedAt: k.CreatedAt.Format("2006-01-02"),
		}
	}

	data := map[string]interface{}{
		"APIKey":        apiKey,
		"Keys":           kv,
		"RateLimit":      rateLimit,
		"RequestsToday":  requestsToday,
		"TokensToday":    int64(0),
		"PercentUsed":    pct,
	}

	renderTemplate(w, "dashboard-consumer.html", data)
}

func (s *Server) handleDashboardDonor(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	donors := s.registry.DonorsForUser(userID)
	stats, _ := s.store.GetDonorStats(userID)
	badge := BadgeForTokens(stats.TotalTokens)
	badgeEmoji := badgeEmoji(badge)

	type agentView struct {
		ProviderID    string
		ModelCount    int
		CurrentLoad   int
		MaxConcurrent int
		Uptime        string
		ModelList     string
	}
	agents := make([]agentView, len(donors))
	for i, d := range donors {
		agents[i] = agentView{
			ProviderID:    d.ProviderID,
			ModelCount:    len(d.Models),
			CurrentLoad:   d.CurrentLoad,
			MaxConcurrent: d.MaxConcurrent,
			Uptime:        formatDuration(time.Since(d.ConnectedAt)),
			ModelList:     joinModels(d.Models),
		}
	}

	// Leaderboard position: count users with more tokens.
	pos := 1
	total := 1
	for _, d := range s.registry.donors {
		if d.UserID == userID {
			continue
		}
		ds, _ := s.store.GetDonorStats(d.UserID)
		total++
		if ds.TotalTokens > stats.TotalTokens {
			pos++
		}
	}

	// Badge progress.
	badgeNext, badgeThreshold := nextBadge(stats.TotalTokens)
	badgePct := 0
	if badgeThreshold > 0 {
		badgePct = int(stats.TotalTokens * 100 / badgeThreshold)
	}
	remaining := badgeThreshold - stats.TotalTokens

	// Find donor-scoped key for token display.
	keys, _ := s.store.ListKeys(userID)
	var tokenPrefix, tokenFull string
	var tokenID int64
	for _, k := range keys {
		if k.Scope == "donor" || k.Scope == "both" {
			tokenPrefix = k.KeyPrefix
			tokenID = k.ID
			break
		}
	}
	if tokenPrefix == "" {
		tokenPrefix = "inf_xxxx"
	}
	tokenFull = tokenPrefix + "..." // can't retrieve full key

	avg := 0.0
	if stats.TotalUptimeSec > 0 {
		avg = float64(stats.TotalTokens) / float64(stats.TotalUptimeSec)
	}
	avgTokensPerSec := fmt.Sprintf("%.1f", avg)

	data := map[string]interface{}{
		"Agents":           agents,
		"Stats":             stats,
		"Badge":             badge,
		"BadgeEmoji":        badgeEmoji,
		"BadgeNext":         badgeNext,
		"BadgeProgress":     stats.TotalTokens,
		"BadgeThreshold":    badgeThreshold,
		"BadgePercent":      badgePct,
		"BadgeRemaining":    remaining,
		"AvgTokensPerSec":   avgTokensPerSec,
		"TokenPrefix":       tokenPrefix,
		"TokenFull":         tokenFull,
		"TokenID":           tokenID,
		"LeaderboardPos":    pos,
		"LeaderboardTotal":  total,
	}

	renderTemplate(w, "dashboard-donor.html", data)
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

func badgeEmoji(badge string) string {
	switch badge {
	case "platinum":
		return "💎"
	case "gold":
		return "🥇"
	case "silver":
		return "🥈"
	case "bronze":
		return "🥉"
	default:
		return "🫐"
	}
}

func nextBadge(tokens int64) (name string, threshold int64) {
	switch {
	case tokens < 1_000:
		return "Bronze", 1_000
	case tokens < 10_000:
		return "Silver", 10_000
	case tokens < 100_000:
		return "Gold", 100_000
	case tokens < 1_000_000:
		return "Platinum", 1_000_000
	default:
		return "Max", 1_000_000
	}
}

func joinModels(models []string) string {
	if len(models) == 0 {
		return ""
	}
	s := models[0]
	for i := 1; i < len(models); i++ {
		s += ", " + models[i]
	}
	return s
}
