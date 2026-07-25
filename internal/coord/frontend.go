package coord

import (
	"fmt"
	"log"
	"net/http"
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


// --- HTMX use fragments ---

// handleUseDonor renders the donor tab content for /use.
func (s *Server) handleUseDonor(w http.ResponseWriter, r *http.Request) {
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
		Description   string
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
			Description:   d.Description,
			ModelCount:    len(d.Models),
			CurrentLoad:   d.CurrentLoad,
			MaxConcurrent: d.MaxConcurrent,
			Uptime:        formatDuration(time.Since(d.ConnectedAt)),
			ModelList:     joinModels(d.Models),
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
	tokenFull = tokenPrefix + "..."

	avg := 0.0
	if stats.TotalUptimeSec > 0 {
		avg = float64(stats.TotalTokens) / float64(stats.TotalUptimeSec)
	}
	avgTokensPerSec := fmt.Sprintf("%.1f", avg)
	totalUptime := formatDuration(time.Duration(stats.TotalUptimeSec) * time.Second)

	data := map[string]interface{}{
		"Agents":           agents,
		"Stats":             stats,
		"TotalUptime":       totalUptime,
		"Badge":             badge,
		"BadgeEmoji":        badgeEmoji,
		"BadgeNext":         badgeNext,
		"BadgeProgress":     stats.TotalTokens,
		"BadgeThreshold":     badgeThreshold,
		"BadgePercent":      badgePct,
		"BadgeRemaining":    remaining,
		"AvgTokensPerSec":   avgTokensPerSec,
		"TokenPrefix":       tokenPrefix,
		"TokenFull":         tokenFull,
		"TokenID":           tokenID,
	}

	renderTemplate(w, "use-donor.html", data)
}

// handleShareStatus renders the share GPU status fragment.
func (s *Server) handleShareStatus(w http.ResponseWriter, r *http.Request) {
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
		Description   string
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
			Description:   d.Description,
			ModelCount:    len(d.Models),
			CurrentLoad:   d.CurrentLoad,
			MaxConcurrent: d.MaxConcurrent,
			Uptime:        formatDuration(time.Since(d.ConnectedAt)),
			ModelList:     joinModels(d.Models),
		}
	}

	badgeNext, badgeThreshold := nextBadge(stats.TotalTokens)
	badgePct := 0
	if badgeThreshold > 0 {
		badgePct = int(stats.TotalTokens * 100 / badgeThreshold)
	}
	remaining := badgeThreshold - stats.TotalTokens

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
	tokenFull = tokenPrefix + "..."

	avg := 0.0
	if stats.TotalUptimeSec > 0 {
		avg = float64(stats.TotalTokens) / float64(stats.TotalUptimeSec)
	}
	avgTokensPerSec := fmt.Sprintf("%.1f", avg)
	totalUptime := formatDuration(time.Duration(stats.TotalUptimeSec) * time.Second)

	data := map[string]interface{}{
		"Agents":           agents,
		"Stats":             stats,
		"TotalUptime":       totalUptime,
		"Badge":             badge,
		"BadgeEmoji":        badgeEmoji,
		"BadgeNext":         badgeNext,
		"BadgeProgress":     stats.TotalTokens,
		"BadgeThreshold":     badgeThreshold,
		"BadgePercent":      badgePct,
		"BadgeRemaining":    remaining,
		"AvgTokensPerSec":   avgTokensPerSec,
		"TokenPrefix":       tokenPrefix,
		"TokenFull":         tokenFull,
		"TokenID":           tokenID,
	}

	renderTemplate(w, "share-status.html", data)
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

// --- HTMX use fragments ---

// handleUseKeys renders the API Keys tab content as an HTMX fragment.
func (s *Server) handleUseKeys(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	keys, _ := s.store.ListKeys(userID)
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
	renderTemplate(w, "use-keys.html", map[string]interface{}{
		"Keys": kv,
	})
}

// handleUseCreateKey creates a new consumer key and returns the API Keys fragment.
func (s *Server) handleUseCreateKey(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	rawKey, _, err := s.store.CreateKey(userID, "consumer")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create key")
		return
	}
	keys, _ := s.store.ListKeys(userID)
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
	renderTemplate(w, "use-keys.html", map[string]interface{}{
		"Keys":    kv,
		"NewKey":  rawKey,
	})
}
