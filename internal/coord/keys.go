package coord

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/r00takaspin/gpumesh/internal/proto"
)

// handleCreateKey creates a new API key for the authenticated user.
func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	// Try query param first, then JSON body.
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		// Parse JSON body for scope.
		var body struct {
			Scope string `json:"scope"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.Scope != "" {
			scope = body.Scope
		}
	}
	if scope == "" {
		scope = proto.ScopeConsumer
	}

	rawKey, keyID, err := s.store.CreateKey(userID, scope)
	if err != nil {
		log.Printf("create key error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create key")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         keyID,
		"key":        rawKey,
		"key_prefix": rawKey[:12],
		"scope":      scope,
		"warning":    "Copy this key now. It will not be shown again.",
	})
}

// handleListKeys returns all API keys for the authenticated user.
func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	keys, err := s.store.ListKeys(userID)
	if err != nil {
		log.Printf("list keys error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list keys")
		return
	}

	type keyEntry struct {
		ID        int64  `json:"id"`
		Prefix    string `json:"prefix"`
		Scope     string `json:"scope"`
		CreatedAt string `json:"created_at"`
	}

	entries := make([]keyEntry, len(keys))
	for i, k := range keys {
		entries[i] = keyEntry{
			ID:        k.ID,
			Prefix:    k.KeyPrefix,
			Scope:     k.Scope,
			CreatedAt: k.CreatedAt.Format("2006-01-02 15:04:05"),
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"keys": entries,
	})
}

// handleRevokeKey revokes an API key. For HTMX requests, re-renders the consumer tab.
func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	keyIDStr := r.PathValue("id")
	keyID, err := strconv.ParseInt(keyIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key id")
		return
	}

	if err := s.store.RevokeKey(userID, keyID); err != nil {
		log.Printf("revoke key error: %v", err)
		writeError(w, http.StatusNotFound, fmt.Sprintf("key not found: %v", err))
		return
	}

	// HTMX: re-render the correct fragment based on source page.
	if r.Header.Get("HX-Request") == "true" {
		if strings.Contains(r.Header.Get("HX-Current-URL"), "/share") {
			s.handleShareDonorStats(w, r)
		} else {
			s.handleUseKeys(w, r)
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

// handleRegenerateKey regenerates an API key (donor or consumer).
func (s *Server) handleRegenerateKey(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	keyIDStr := r.PathValue("id")
	keyID, err := strconv.ParseInt(keyIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key id")
		return
	}

	// Look up the original key to preserve its scope.
	oldKey, err := s.store.FindKeyByID(keyID)
	if err != nil {
		log.Printf("regenerate: find key error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to find key")
		return
	}
	if oldKey == nil {
		writeError(w, http.StatusNotFound, "key not found")
		return
	}

	// Revoke old key.
	if err := s.store.RevokeKey(userID, keyID); err != nil {
		log.Printf("regenerate: revoke error: %v", err)
		writeError(w, http.StatusNotFound, "key not found")
		return
	}

	// Create new key with same scope.
	rawKey, newKeyID, err := s.store.CreateKey(userID, oldKey.Scope)
	if err != nil {
		log.Printf("regenerate: create error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create key")
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Trigger", "refreshStats")
		renderTemplate(w, "share-token-modal.html", map[string]interface{}{
			"Token": rawKey,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         newKeyID,
		"key":        rawKey,
		"key_prefix": rawKey[:12],
		"scope":      oldKey.Scope,
		"warning":    "Copy this key now. It will not be shown again.",
	})
}
