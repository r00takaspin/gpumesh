package coord

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// pinLimiter rate-limits PIN redeem attempts by IP and user.
type pinLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	attempts map[string][]time.Time
}

func newPinLimiter(limit int) *pinLimiter {
	if limit <= 0 {
		limit = 10
	}
	return &pinLimiter{
		limit:    limit,
		window:   15 * time.Minute,
		attempts: make(map[string][]time.Time),
	}
}

func (p *pinLimiter) allow(keys ...string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-p.window)
	for _, key := range keys {
		if key == "" {
			continue
		}
		times := p.attempts[key]
		filtered := times[:0]
		for _, t := range times {
			if t.After(cutoff) {
				filtered = append(filtered, t)
			}
		}
		p.attempts[key] = filtered
		if len(filtered) >= p.limit {
			return false
		}
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		p.attempts[key] = append(p.attempts[key], now)
	}
	return true
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// --- POST /api/invites ---

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	var body struct {
		MachineID string `json:"machine_id"`
		MaxUses   int    `json:"max_uses"`
		TTLDays   int    `json:"ttl_days"`
		Label     string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.MachineID == "" {
		writeError(w, http.StatusBadRequest, "machine_id is required")
		return
	}
	if body.MaxUses == 0 {
		body.MaxUses = s.inviteMaxUses
	}
	if body.TTLDays == 0 {
		body.TTLDays = s.inviteTTLDays
	}

	inv, pin, err := s.store.CreateInvite(body.MachineID, userID, body.MaxUses, body.TTLDays, body.Label)
	if err != nil {
		log.Printf("create invite: %v", err)
		writeError(w, http.StatusForbidden, "cannot create invite for this machine")
		return
	}

	joinLink := strings.TrimRight(s.baseURL, "/") + "/join?pin=" + pin
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         inv.ID,
		"machine_id": inv.MachineID,
		"pin":        pin,
		"join_link":  joinLink,
		"max_uses":   inv.MaxUses,
		"expires_at": inv.ExpiresAt.UTC().Format(time.RFC3339),
		"label":      inv.Label,
		"warning":    "Copy this PIN now. It will not be shown again.",
	})
}

// --- GET /api/invites ---

func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	invites, err := s.store.ListInvitesByOwner(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list invites")
		return
	}
	entries := make([]map[string]interface{}, 0, len(invites))
	for _, inv := range invites {
		entries = append(entries, map[string]interface{}{
			"id":         inv.ID,
			"machine_id": inv.MachineID,
			"pin_masked": inv.MaskedPIN(),
			"max_uses":   inv.MaxUses,
			"uses":       inv.Uses,
			"expires_at": inv.ExpiresAt.UTC().Format(time.RFC3339),
			"status":     inv.Status(),
			"label":      inv.Label,
			"created_at": inv.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"invites": entries})
}

// --- DELETE /api/invites/{id} ---

func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid invite id")
		return
	}
	if err := s.store.RevokeInvite(userID, id); err != nil {
		writeError(w, http.StatusNotFound, "invite not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

// --- POST /api/join ---

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	var body struct {
		PIN string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ip := clientIP(r)
	userKey := fmt.Sprintf("user:%d", userID)
	ipKey := "ip:" + ip
	if !s.pinLimiter.allow(userKey, ipKey) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
		return
	}

	machineID, _, err := s.store.RedeemPIN(userID, body.PIN)
	if err != nil {
		code := err.Error()
		if errors.Is(err, ErrInvalidPin) || errors.Is(err, ErrExpiredPin) ||
			errors.Is(err, ErrExhausted) || errors.Is(err, ErrRevokedPin) ||
			errors.Is(err, ErrMachineGone) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": code})
			return
		}
		log.Printf("join error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	rawKey, keyID, created, err := s.store.EnsureConsumerKey(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to ensure API key")
		return
	}

	resp := map[string]interface{}{
		"machine_id": machineID,
		"base_url":   strings.TrimRight(s.baseURL, "/") + "/v1/machines/" + machineID,
		"key_id":     keyID,
	}
	if created {
		resp["api_key"] = rawKey
		resp["warning"] = "Copy this key now. It will not be shown again."
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- GET /api/bindings ---

func (s *Server) handleListBindings(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	list, err := s.store.ListAccessibleMachines(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list bindings")
		return
	}
	entries := make([]map[string]interface{}, 0, len(list))
	for _, bi := range list {
		sess := s.registry.GetSession(bi.MachineID)
		online := sess != nil && sess.BackendOK
		load := 0.0
		models := []string{}
		if sess != nil {
			load = sess.LoadFraction()
			models = append(models, sess.Models...)
		}
		entries = append(entries, map[string]interface{}{
			"machine_id":   bi.MachineID,
			"machine_name": bi.DisplayName,
			"role":         bi.Role,
			"online":       online,
			"load":         load,
			"models":       models,
			"base_url":     strings.TrimRight(s.baseURL, "/") + "/v1/machines/" + bi.MachineID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"bindings": entries})
}

// --- DELETE /api/bindings/{machine_id} ---

func (s *Server) handleRevokeBinding(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	machineID := r.PathValue("machine_id")
	if err := s.store.RevokeBindingByMember(userID, machineID); err != nil {
		writeError(w, http.StatusNotFound, "binding not found")
		return
	}
	s.cancelMachineRequests(machineID)
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

// --- DELETE /api/machines/{machine_id}/members/{user_id} ---

func (s *Server) handleRevokeMember(w http.ResponseWriter, r *http.Request) {
	ownerID := getUserID(r)
	machineID := r.PathValue("machine_id")
	memberID, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := s.store.RevokeMemberByOwner(ownerID, machineID, memberID); err != nil {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}
	s.cancelMachineRequests(machineID)
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

// cancelMachineRequests cancels in-flight SSE/relay for a machine after binding revoke.
func (s *Server) cancelMachineRequests(machineID string) {
	if sess := s.registry.GetSession(machineID); sess != nil {
		sess.CancelAllRequests()
	}
}

// --- GET /join (stub page until Light calm UI) ---

func (s *Server) handleJoinPage(w http.ResponseWriter, r *http.Request) {
	pd := s.pageDataWithStats(r)
	pd.Tab = "join"
	// Reuse about template shell if join.html missing — render minimal JSON-friendly page.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	pin := r.URL.Query().Get("pin")
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Join — GPU Mesh</title></head><body>
<h1>Join a machine</h1>
<p>Redeem a PIN after signing in with GitHub.</p>
<form id="join-form"><input name="pin" value="%s" placeholder="XXXX-XXXX"/><button type="submit">Join</button></form>
<pre id="out"></pre>
<script>
document.getElementById('join-form').onsubmit=async(e)=>{
  e.preventDefault();
  const pin=e.target.pin.value;
  const r=await fetch('/api/join',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({pin})});
  document.getElementById('out').textContent=await r.text();
  if(r.ok) location.href='/use';
};
</script>
<p><a href="/login?redirect=/join">Sign in with GitHub</a></p>
</body></html>`, htmlEscape(pin))
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
