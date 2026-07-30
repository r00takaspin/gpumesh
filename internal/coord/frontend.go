package coord

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type machineStripView struct {
	ID         string
	Name       string
	Hardware   string
	Online     bool
	ModelCount int
	Models     string
}

type inviteListView struct {
	ID         int64
	MaskedPIN  string
	Status     string
	Uses       int
	MaxUses    int
	ExpiresRel string
}

type memberView struct {
	UserID    int64
	Login     string
	MachineID string
	JoinedRel string
}

type useMachineView struct {
	ID           string
	Name         string
	OwnerLabel   string
	Role         string
	Online       bool
	Models       string
	BaseURL      string
	DefaultModel string
}

// --- JSON APIs (owner/consumer) ---

func (s *Server) handleConsumerStats(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	keys, _ := s.store.ListKeys(userID)
	rateLimit := s.limiter.Burst()
	minRemaining := rateLimit
	for _, k := range keys {
		if rem := s.limiter.Remaining(k.KeyHash); rem < minRemaining {
			minRemaining = rem
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"requests_today": rateLimit - minRemaining,
		"tokens_today":   int64(0),
		"rate_limit":     rateLimit,
		"rate_remaining": minRemaining,
	})
}

func (s *Server) handleOwnerStatsAPI(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	ds, err := s.store.GetOwnerStats(userID)
	if err != nil {
		log.Printf("owner stats error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_requests":       ds.TotalRequests,
		"total_tokens":         ds.TotalTokens,
		"total_uptime_seconds": ds.TotalUptimeSec,
		"badge":                BadgeForTokens(ds.TotalTokens),
	})
}

func (s *Server) handleOwnerStatus(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	donors := s.registry.MachinesForUser(userID)
	agents := make([]map[string]interface{}, 0, len(donors))
	for _, d := range donors {
		uptime := time.Since(d.ConnectedAt)
		agents = append(agents, map[string]interface{}{
			"machine_id":  d.MachineID,
			"online":      true,
			"models":      d.Models,
			"load":        fmt.Sprintf("%d/%d", d.CurrentLoad, d.MaxConcurrent),
			"uptime":      formatDuration(uptime),
			"description": d.Description,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"agents": agents})
}

// --- Share progressive panel ---

func (s *Server) handleSharePanel(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	keys, _ := s.store.ListKeys(userID)
	var providerKeyID int64
	var tokenPrefix string
	for _, k := range keys {
		if k.Scope == "provider" || k.Scope == "donor" || k.Scope == "both" {
			providerKeyID = k.ID
			tokenPrefix = k.KeyPrefix
			break
		}
	}

	base := strings.TrimRight(s.baseURL, "/")
	runCmd := fmt.Sprintf("export MESH_COORDINATOR=%q\nexport MESH_TOKEN=%q\ngpumesh-provider",
		base, tokenPrefix+"…")

	if providerKeyID == 0 {
		renderTemplate(w, "share-panel.html", map[string]interface{}{
			"State":   "no-token",
			"BaseURL": base,
		})
		return
	}

	machines, _ := s.store.ListMachinesByOwner(userID)
	if len(machines) == 0 {
		renderTemplate(w, "share-panel.html", map[string]interface{}{
			"State":         "waiting",
			"BaseURL":       base,
			"RunCommand":    runCmd,
			"TokenPrefix":   tokenPrefix,
			"ProviderKeyID": providerKeyID,
		})
		return
	}

	views := make([]machineStripView, 0, len(machines))
	anyOnline := false
	for _, m := range machines {
		sess := s.registry.GetSession(m.ID)
		online := sess != nil && sess.BackendOK
		if online {
			anyOnline = true
		}
		hw := ""
		models := ""
		count := 0
		if sess != nil {
			hw = sess.Hardware
			models = joinModels(sess.Models)
			count = len(sess.Models)
		}
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}
		views = append(views, machineStripView{
			ID: m.ID, Name: name, Hardware: hw, Online: online, ModelCount: count, Models: models,
		})
	}

	invites, _ := s.store.ListInvitesByOwner(userID)
	invViews := make([]inviteListView, 0, len(invites))
	for _, inv := range invites {
		invViews = append(invViews, inviteListView{
			ID:         inv.ID,
			MaskedPIN:  inv.MaskedPIN(),
			Status:     inv.Status(),
			Uses:       inv.Uses,
			MaxUses:    inv.MaxUses,
			ExpiresRel: formatExpiresRel(inv.ExpiresAt),
		})
	}

	selected := views[0].ID
	renderTemplate(w, "share-panel.html", map[string]interface{}{
		"State":             "ready",
		"Offline":           !anyOnline,
		"Machines":          views,
		"SelectedMachineID": selected,
		"Invites":           invViews,
		"BaseURL":           base,
		"RunCommand":        runCmd,
		"TokenPrefix":       tokenPrefix,
		"ProviderKeyID":     providerKeyID,
	})
}

func (s *Server) handleShareMembers(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	machineID := r.URL.Query().Get("machine_id")
	if machineID == "" {
		machines, _ := s.store.ListMachinesByOwner(userID)
		if len(machines) == 0 {
			renderTemplate(w, "share-members.html", map[string]interface{}{"Members": nil})
			return
		}
		machineID = machines[0].ID
	}
	bindings, err := s.store.ListMembers(machineID, userID)
	if err != nil {
		renderTemplate(w, "share-members.html", map[string]interface{}{"Members": nil})
		return
	}
	members := make([]memberView, 0, len(bindings))
	for _, b := range bindings {
		login, _ := s.store.GetUserByID(b.MemberUserID)
		if login == "" {
			login = fmt.Sprintf("user-%d", b.MemberUserID)
		}
		members = append(members, memberView{
			UserID:    b.MemberUserID,
			Login:     login,
			MachineID: machineID,
			JoinedRel: formatRelative(b.CreatedAt),
		})
	}
	renderTemplate(w, "share-members.html", map[string]interface{}{"Members": members})
}

func (s *Server) handleShareCreateInvite(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	machineID := r.FormValue("machine_id")
	maxUses, _ := strconv.Atoi(r.FormValue("max_uses"))
	ttlDays, _ := strconv.Atoi(r.FormValue("ttl_days"))
	if machineID == "" {
		writeError(w, http.StatusBadRequest, "machine_id is required")
		return
	}
	if maxUses == 0 {
		maxUses = s.inviteMaxUses
	}
	if ttlDays == 0 {
		ttlDays = s.inviteTTLDays
	}

	inv, pin, err := s.store.CreateInvite(machineID, userID, maxUses, ttlDays, "")
	if err != nil {
		log.Printf("create invite: %v", err)
		writeError(w, http.StatusForbidden, "cannot create invite for this machine")
		return
	}

	machineName := machineID
	if m, _ := s.store.GetMachine(machineID); m != nil && m.DisplayName != "" {
		machineName = m.DisplayName
	}
	joinLink := strings.TrimRight(s.baseURL, "/") + "/join?pin=" + pin
	w.Header().Set("HX-Trigger", "refreshPanel")
	renderTemplate(w, "share-invite-modal.html", map[string]interface{}{
		"PIN":         pin,
		"JoinLink":    joinLink,
		"MaxUses":     inv.MaxUses,
		"TTLDays":     ttlDays,
		"MachineName": machineName,
	})
}

func (s *Server) handleShareCreateToken(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	rawKey, _, err := s.store.CreateKey(userID, "provider")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	base := strings.TrimRight(s.baseURL, "/")
	coordURL := providerWSURL(base)
	w.Header().Set("HX-Trigger", "refreshPanel")
	renderTemplate(w, "share-token-modal.html", map[string]interface{}{
		"Token":           rawKey,
		"CoordinatorURL":  coordURL,
		"CoordinatorHTTP": base,
	})
}

// --- Use machines / keys ---

func (s *Server) handleUseMachines(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	list, err := s.store.ListAccessibleMachines(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list machines")
		return
	}
	base := strings.TrimRight(s.baseURL, "/")
	views := make([]useMachineView, 0, len(list))
	for _, bi := range list {
		sess := s.registry.GetSession(bi.MachineID)
		online := sess != nil && sess.BackendOK
		models := ""
		defaultModel := "llama3.2:3b"
		if sess != nil && len(sess.Models) > 0 {
			models = joinModels(sess.Models)
			defaultModel = sess.Models[0]
		}
		ownerLabel := "owned by you"
		if bi.Role == "member" {
			login, _ := s.store.GetUserByID(bi.OwnerUserID)
			if login != "" {
				ownerLabel = "@" + login
			} else {
				ownerLabel = "member"
			}
		}
		name := bi.DisplayName
		if name == "" {
			name = bi.MachineID
		}
		views = append(views, useMachineView{
			ID:           bi.MachineID,
			Name:         name,
			OwnerLabel:   ownerLabel,
			Role:         bi.Role,
			Online:       online,
			Models:       models,
			BaseURL:      base + "/v1/machines/" + bi.MachineID,
			DefaultModel: defaultModel,
		})
	}
	renderTemplate(w, "use-machines.html", map[string]interface{}{"Machines": views})
}

func (s *Server) handleUseKeys(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	renderTemplate(w, "use-keys.html", s.keysViewData(userID, ""))
}

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
	renderTemplate(w, "use-keys.html", s.keysViewData(userID, rawKey))
}

func (s *Server) keysViewData(userID int64, newKey string) map[string]interface{} {
	keys, _ := s.store.ListKeys(userID)
	type keyView struct {
		ID         int64
		Prefix     string
		Scope      string
		ScopeLabel string
		CreatedAt  string
	}
	var kv []keyView
	for _, k := range keys {
		label := "for tools"
		switch k.Scope {
		case "provider", "donor":
			label = "for provider"
		case "both":
			label = "for tools & provider"
		}
		kv = append(kv, keyView{
			ID:         k.ID,
			Prefix:     k.KeyPrefix,
			Scope:      k.Scope,
			ScopeLabel: label,
			CreatedAt:  k.CreatedAt.Format("2006-01-02"),
		})
	}
	return map[string]interface{}{
		"Keys":   kv,
		"NewKey": newKey,
	}
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

func providerWSURL(base string) string {
	if strings.Contains(base, "localhost") || strings.Contains(base, "127.0.0.1") {
		return "ws://" + strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://") + "/ws/provider"
	}
	if strings.HasPrefix(base, "https://") {
		return "wss://" + strings.TrimPrefix(base, "https://") + "/ws/provider"
	}
	return "ws://" + strings.TrimPrefix(base, "http://") + "/ws/provider"
}
