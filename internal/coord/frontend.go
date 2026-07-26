package coord

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// donorView is the shared view model for donor agent cards.
type donorView struct {
	ProviderID      string
	Description     string
	Hardware        string
	ModelCount      int
	CurrentLoad     int
	MaxConcurrent   int
	Uptime          string
	ModelList       string
	SessionRequests int
	SessionTokens   int
	TokPerSec       string
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

	agents := make([]donorView, len(donors))
	for i, d := range donors {
		uptime := time.Since(d.ConnectedAt)
		var tokPerSec string
		if uptime.Seconds() > 0 && d.SessionTokens > 0 {
			tokPerSec = fmt.Sprintf("%.1f", float64(d.SessionTokens)/uptime.Seconds())
		} else {
			tokPerSec = "—"
		}
		agents[i] = donorView{
			ProviderID:      d.ProviderID,
			Description:     d.Description,
			Hardware:        d.Hardware,
			ModelCount:      len(d.Models),
			CurrentLoad:     d.CurrentLoad,
			MaxConcurrent:   d.MaxConcurrent,
			Uptime:          formatDuration(uptime),
			ModelList:       joinModels(d.Models),
			SessionRequests: d.SessionRequests,
			SessionTokens:   d.SessionTokens,
			TokPerSec:       tokPerSec,
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
	// Collect all donor/both keys for the token list.
	type donorKeyView struct {
		ID        int64
		KeyPrefix string
		CreatedAt string
	}
	keys, _ := s.store.ListKeys(userID)
	var donorKeys []donorKeyView
	for _, k := range keys {
		if k.Scope == "donor" || k.Scope == "both" {
			donorKeys = append(donorKeys, donorKeyView{
				ID:        k.ID,
				KeyPrefix: k.KeyPrefix,
				CreatedAt: k.CreatedAt.Format("2006-01-02"),
			})
		}
	}

	tokenPrefix := "inf_xxxx"
	if len(donorKeys) > 0 {
		tokenPrefix = donorKeys[0].KeyPrefix
	}
	tokenFull := tokenPrefix + "..."

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
		"DonorKeys":         donorKeys,
	}

	renderTemplate(w, "use-donor.html", data)
}
const ctxKeyNewToken contextKey = "newToken"

// handleShareSetup renders the share setup/onboarding block.
func (s *Server) handleShareSetup(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	donors := s.registry.DonorsForUser(userID)

	// Build coordinator WebSocket URL.
	coordURL := s.baseURL
	if strings.Contains(coordURL, "localhost") || strings.Contains(coordURL, "127.0.0.1") {
		coordURL = "ws://" + strings.TrimPrefix(strings.TrimPrefix(coordURL, "https://"), "http://") + "/ws/provider"
	} else if strings.HasPrefix(coordURL, "https://") {
		coordURL = "wss://" + strings.TrimPrefix(coordURL, "https://") + "/ws/provider"
	} else {
		coordURL = "ws://" + strings.TrimPrefix(coordURL, "http://") + "/ws/provider"
	}

	// Find the first donor-scoped key for the run command.
	keys, _ := s.store.ListKeys(userID)
	var token string
	for _, k := range keys {
		if k.Scope == "donor" || k.Scope == "both" {
			token = k.KeyPrefix + "..."
			break
		}
	}

	newTokenFull, _ := r.Context().Value(ctxKeyNewToken).(string)

	data := map[string]interface{}{
		"CoordinatorURL": coordURL,
		"Token":          token,
		"NewTokenFull":   newTokenFull,
		"HasDonors":      len(donors) > 0,
		"HasToken":       token != "",
		"ActiveTab":      r.URL.Query().Get("os-tab-share"),
	}

	renderTemplate(w, "share-setup.html", data)
}

// handleShareModels renders the donor model/hardware status cards.
func (s *Server) handleShareModels(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	donors := s.registry.DonorsForUser(userID)
	sort.Slice(donors, func(i, j int) bool { return donors[i].Description < donors[j].Description })
	agents := make([]donorView, len(donors))
	for i, d := range donors {
		uptime := time.Since(d.ConnectedAt)
		var tokPerSec string
		if uptime.Seconds() > 0 && d.SessionTokens > 0 {
			tokPerSec = fmt.Sprintf("%.1f", float64(d.SessionTokens)/uptime.Seconds())
		} else {
			tokPerSec = "—"
		}
		agents[i] = donorView{
			ProviderID:      d.ProviderID,
			Description:     d.Description,
			Hardware:        d.Hardware,
			ModelCount:      len(d.Models),
			CurrentLoad:     d.CurrentLoad,
			MaxConcurrent:   d.MaxConcurrent,
			Uptime:          formatDuration(uptime),
			ModelList:       joinModels(d.Models),
			SessionRequests: d.SessionRequests,
			SessionTokens:   d.SessionTokens,
			TokPerSec:       tokPerSec,
		}
	}

	data := map[string]interface{}{
		"Agents": agents,
	}

	renderTemplate(w, "share-models.html", data)
}

// handleShareDonorStats renders stats + badge + token list (collapsed by default).
func (s *Server) handleShareDonorStats(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	donors := s.registry.DonorsForUser(userID)
	stats, _ := s.store.GetDonorStats(userID)

	// Only render if there's activity or donors connected.
	// Always render — token management UI must be visible even with no stats.
	_ = donors
	_ = stats

	badge := BadgeForTokens(stats.TotalTokens)
	badgeEmoji := badgeEmoji(badge)

	badgeNext, badgeThreshold := nextBadge(stats.TotalTokens)
	badgePct := 0
	if badgeThreshold > 0 {
		badgePct = int(stats.TotalTokens * 100 / badgeThreshold)
	}
	remaining := badgeThreshold - stats.TotalTokens

	// Collect donor keys for the token list.
	type donorKeyView struct {
		ID        int64
		KeyPrefix string
		CreatedAt string
	}
	keys, _ := s.store.ListKeys(userID)
	var donorKeys []donorKeyView
	for _, k := range keys {
		if k.Scope == "donor" || k.Scope == "both" {
			donorKeys = append(donorKeys, donorKeyView{
				ID:        k.ID,
				KeyPrefix: k.KeyPrefix,
				CreatedAt: k.CreatedAt.Format("2006-01-02"),
			})
		}
	}

	avg := 0.0
	if stats.TotalUptimeSec > 0 {
		avg = float64(stats.TotalTokens) / float64(stats.TotalUptimeSec)
	}
	avgTokensPerSec := fmt.Sprintf("%.1f", avg)
	totalUptime := formatDuration(time.Duration(stats.TotalUptimeSec) * time.Second)

	data := map[string]interface{}{
		"Stats":           stats,
		"TotalUptime":     totalUptime,
		"Badge":           badge,
		"BadgeEmoji":      badgeEmoji,
		"BadgeNext":       badgeNext,
		"BadgeProgress":   stats.TotalTokens,
		"BadgeThreshold":  badgeThreshold,
		"BadgePercent":    badgePct,
		"BadgeRemaining":  remaining,
		"AvgTokensPerSec": avgTokensPerSec,
		"DonorKeys":       donorKeys,
	}

	renderTemplate(w, "share-stats.html", data)
}

// handleShareCreateToken creates a new donor token and re-renders the setup fragment.
func (s *Server) handleShareCreateToken(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	rawKey, _, err := s.store.CreateKey(userID, "donor")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	w.Header().Set("HX-Trigger", "refreshStats")
	ctx := context.WithValue(r.Context(), ctxKeyNewToken, rawKey)
	s.handleShareSetup(w, r.WithContext(ctx))
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
	var kv []keyView
	for _, k := range keys {
		if k.Scope == "consumer" || k.Scope == "both" {
			kv = append(kv, keyView{
				ID:        k.ID,
				Prefix:    k.KeyPrefix,
				Scope:     k.Scope,
				CreatedAt: k.CreatedAt.Format("2006-01-02"),
			})
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
	kv := make([]keyView, 0, len(keys))
	for _, k := range keys {
		if k.Scope == "consumer" || k.Scope == "both" {
			kv = append(kv, keyView{
				ID:        k.ID,
				Prefix:    k.KeyPrefix,
				Scope:     k.Scope,
				CreatedAt: k.CreatedAt.Format("2006-01-02"),
			})
		}
	}
	renderTemplate(w, "use-keys.html", map[string]interface{}{
		"Keys":    kv,
		"NewKey":  rawKey,
	})
}
