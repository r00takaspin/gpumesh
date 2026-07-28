package coord

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/r00takaspin/gpumesh/internal/proto"
)

// --- GET /v1/models ---

func (s *Server) handleAPIModels(w http.ResponseWriter, r *http.Request) {
	// Rate limit check (auth already done by requireAPIKey middleware).
	keyHash, _ := r.Context().Value(ctxKeyAPIKeyHash).(string)
	if keyHash != "" {
		allowed, remaining := s.limiter.Allow(keyHash)
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		if !allowed {
			w.Header().Set("Retry-After", "3600")
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
	}

	snap := s.registry.Snapshot()

	resp := proto.ModelListResponse{
		Object: "list",
		Data:   make([]proto.ModelEntry, 0, len(snap.Models)),
	}

	for name, ms := range snap.Models {
		resp.Data = append(resp.Data, proto.ModelEntry{
			ID:           name,
			Object:       "model",
			OwnedBy:      "community",
			DonorsOnline: ms.DonorsOnline,
			Load:         ms.Load,
		})
	}

	sort.Slice(resp.Data, func(i, j int) bool {
		return resp.Data[i].ID < resp.Data[j].ID
	})

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAPIChatCompletions(w http.ResponseWriter, r *http.Request) {
	// Auth already done by requireAPIKey middleware; key hash is in context.
	keyHash, _ := r.Context().Value(ctxKeyAPIKeyHash).(string)

	// Rate limit.
	allowed, remaining := s.limiter.Allow(keyHash)
	w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
	if !allowed {
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	// Parse request body.
	var req proto.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	// Increment global daily counter.
	atomic.AddInt64(&s.requestsToday, 1)

	// Find donors for the model.
	donors := s.registry.FindDonorsForModel(req.Model)
	if len(donors) == 0 {
		// Try stripping a provider prefix (e.g. "openai/llama3.2" → "llama3.2")
		if idx := strings.IndexByte(req.Model, '/'); idx >= 0 {
			req.Model = req.Model[idx+1:]
			donors = s.registry.FindDonorsForModel(req.Model)
		}
	}
	if len(donors) == 0 {
		snap := s.registry.Snapshot()
		models := make([]string, 0, len(snap.Models))
		for m := range snap.Models {
			models = append(models, m)
		}
		sort.Strings(models)
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":            "Model not available",
			"available_models": models,
		})
		return
	}

	// Sticky session: prefer the same donor for KV-cache reuse.
	userID := getUserID(r)
	var selected *Donor
	if userID != 0 {
		s.affinityMu.RLock()
		aff, ok := s.affinity[userID]
		s.affinityMu.RUnlock()
		if ok && aff.Model == req.Model && time.Now().Before(aff.ExpiresAt) {
			for _, d := range donors {
				if d.ProviderID == aff.ProviderID && d.CurrentLoad < d.MaxConcurrent {
					selected = d
					break
				}
			}
		}
	}

	// Fallback: sort by load, pick least-loaded donor with capacity.
	if selected == nil {
		sort.Slice(donors, func(i, j int) bool {
			li := float64(donors[i].CurrentLoad) / float64(donors[i].MaxConcurrent)
			lj := float64(donors[j].CurrentLoad) / float64(donors[j].MaxConcurrent)
			return li < lj
		})
		for _, d := range donors {
			if d.CurrentLoad < d.MaxConcurrent {
				selected = d
				break
			}
		}
	}
	if selected == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":                "All donors busy",
			"retry_after_seconds":  30,
		})
		return
	}

	// Refresh affinity.
	if userID != 0 {
		ttl := s.affinityTTL
		if ttl == 0 {
			ttl = 2 * time.Minute
		}
		s.affinityMu.Lock()
		s.affinity[userID] = consumerAffinity{
			ProviderID: selected.ProviderID,
			Model:      req.Model,
			ExpiresAt:  time.Now().Add(ttl),
		}
		s.affinityMu.Unlock()
	}

	// Generate request_id.
	requestID := generateRequestID()

	if req.Stream {
		s.handleStreamingCompletion(w, r, selected, &req, requestID)
	} else {
		s.handleNonStreamingCompletion(w, r, selected, &req, requestID)
	}
}


// handleStreamingCompletion handles a streaming chat completion request.
func (s *Server) handleStreamingCompletion(w http.ResponseWriter, r *http.Request, donor *Donor, req *proto.ChatCompletionRequest, requestID string) {
	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("chat: streaming not supported")
		return
	}

	// Create relay channel.
	ch := make(chan ChunkRelay, 32)
	donor.RegisterChunkChannel(requestID, ch)
	defer donor.UnregisterChunkChannel(requestID)

	// Increment load (decremented on all exit paths below).
	if !s.registry.IncrementLoad(donor.ProviderID) {
		donor.UnregisterChunkChannel(requestID)
		_, _ = fmt.Fprintf(w, "data: {\"error\":\"donor overloaded\"}\n\n")
		flusher.Flush()
		return
	}

	// Send request to donor.
	requestMsg := proto.RequestMsg{
		Type:      proto.TypeRequest,
		RequestID: requestID,
		Model:     req.Model,
		Messages:  req.Messages,
		Stream:    true,
		Options:   buildOptions(req),
	}
	if err := donor.SendWS(requestMsg); err != nil {
		log.Printf("chat: send request error: %v", err)
		s.registry.DecrementLoad(donor.ProviderID)
		_, _ = fmt.Fprintf(w, "data: {\"error\":\"donor unavailable\"}\n\n")
		flusher.Flush()
		return
	}

	// Total request timeout.
	totalCtx, totalCancel := context.WithTimeout(r.Context(), proto.TotalRequestTimeout)
	defer totalCancel()

	// TTFT timer.
	ttftTimer := time.NewTimer(proto.TTFTTimeout)
	defer ttftTimer.Stop()

	// Inter-token timer.
	interTokenTimer := time.NewTimer(proto.InterTokenTimeout)
	interTokenTimer.Stop()
	var firstTokenReceived bool

	created := time.Now().Unix()

	for {
		select {
		case <-totalCtx.Done():
			// Send cancel to donor.
			if err := donor.SendWS(proto.CancelMsg{Type: proto.TypeCancel, RequestID: requestID}); err != nil {
				log.Printf("send cancel: %v", err)
			}
			if firstTokenReceived {
				// SPEC §3.7: return generated tokens with finish_reason "length".
				finalChunk := map[string]interface{}{
					"id":      "chatcmpl-" + requestID,
					"object":  "chat.completion.chunk",
					"created": created,
					"model":   req.Model,
					"choices": []map[string]interface{}{
						{
							"index":         0,
							"delta":         map[string]string{},
							"finish_reason": "length",
						},
					},
				}
				data, _ := json.Marshal(finalChunk)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
			s.sendSSEDone(w, flusher)
			s.registry.DecrementLoad(donor.ProviderID)
			return

		case <-r.Context().Done():
			// Consumer disconnected.
			if err := donor.SendWS(proto.CancelMsg{Type: proto.TypeCancel, RequestID: requestID}); err != nil {
				log.Printf("send cancel: %v", err)
			}
			s.registry.DecrementLoad(donor.ProviderID)
			return

		case <-ttftTimer.C:
			// TTFT timeout.
			if err := donor.SendWS(proto.CancelMsg{Type: proto.TypeCancel, RequestID: requestID}); err != nil {
				log.Printf("send cancel: %v", err)
			}
			_, _ = fmt.Fprintf(w, "data: {\"error\":\"timeout waiting for first token\"}\n\n")
			flusher.Flush()
			s.registry.DecrementLoad(donor.ProviderID)
			return

		case <-interTokenTimer.C:
			// Inter-token timeout.
			_ = donor.SendWS(proto.CancelMsg{Type: proto.TypeCancel, RequestID: requestID})
			_, _ = fmt.Fprintf(w, "data: {\"error\":\"inter-token timeout\"}\n\n")
			flusher.Flush()
			s.registry.DecrementLoad(donor.ProviderID)
			return

		case cr, ok := <-ch:
			if !ok {
				// Channel closed (donor disconnected).
				_, _ = fmt.Fprintf(w, "data: {\"error\":\"donor disconnected\"}\n\n")
				flusher.Flush()
				s.registry.DecrementLoad(donor.ProviderID)
				return
			}
			if cr.Err != "" {
				_, _ = fmt.Fprintf(w, "data: {\"error\":%q}\n\n", cr.Err)
				flusher.Flush()
				s.registry.DecrementLoad(donor.ProviderID)
				return
			}
			if !firstTokenReceived {
				firstTokenReceived = true
				ttftTimer.Stop()
				// Start inter-token timer now that we have the first token.
				interTokenTimer.Reset(proto.InterTokenTimeout)
			} else {
				// Reset inter-token timer.
				if !interTokenTimer.Stop() {
					select {
					case <-interTokenTimer.C:
					default:
					}
				}
				interTokenTimer.Reset(proto.InterTokenTimeout)
			}

			// Write SSE chunk in OpenAI format.
			chunk := map[string]interface{}{
				"id":      "chatcmpl-" + requestID,
				"object":  "chat.completion.chunk",
				"created": created,
				"model":   req.Model,
				"choices": []map[string]interface{}{
					{
						"index":         0,
						"delta":         map[string]string{"content": cr.Content},
						"finish_reason": nil,
					},
				},
			}
			if cr.Done {
				chunk["choices"].([]map[string]interface{})[0]["finish_reason"] = "stop"
			}
			data, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

			if cr.Done {
			_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				s.registry.DecrementLoad(donor.ProviderID)
				return
			}
		}
	}
}

// handleNonStreamingCompletion handles a non-streaming chat completion request.
func (s *Server) handleNonStreamingCompletion(w http.ResponseWriter, r *http.Request, donor *Donor, req *proto.ChatCompletionRequest, requestID string) {
	// For non-streaming, we try up to MaxRetries donors (§3.5).
	for attempt := range proto.MaxRetries {
		if attempt > 0 {
			// Retry on another donor.
			donors := s.registry.FindDonorsForModel(req.Model)
			sort.Slice(donors, func(i, j int) bool {
				li := float64(donors[i].CurrentLoad) / float64(donors[i].MaxConcurrent)
				lj := float64(donors[j].CurrentLoad) / float64(donors[j].MaxConcurrent)
				return li < lj
			})
			var found *Donor
			for _, d := range donors {
				if d.CurrentLoad < d.MaxConcurrent {
					found = d
					break
				}
			}
			if found == nil {
				break
			}
			donor = found
			requestID = generateRequestID()
		}

		result, err := s.sendNonStreamingRequest(r.Context(), donor, req, requestID)
		if err == nil {
			result["id"] = "chatcmpl-" + requestID
			result["object"] = "chat.completion"
			result["created"] = time.Now().Unix()
			result["model"] = req.Model
			writeJSON(w, http.StatusOK, result)
			return
		}
		log.Printf("chat: non-streaming attempt %d/%d failed: %v", attempt+1, proto.MaxRetries, err)
	}

	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "all donors failed"})
}

// sendNonStreamingRequest sends a non-streaming request and waits for response.
func (s *Server) sendNonStreamingRequest(ctx context.Context, donor *Donor, req *proto.ChatCompletionRequest, requestID string) (map[string]interface{}, error) {
	if !s.registry.IncrementLoad(donor.ProviderID) {
		return nil, fmt.Errorf("donor overloaded")
	}
	defer s.registry.DecrementLoad(donor.ProviderID)

	// Use the chunk relay mechanism for the response.
	ch := make(chan ChunkRelay, 1)
	donor.RegisterChunkChannel(requestID, ch)
	defer donor.UnregisterChunkChannel(requestID)

	requestMsg := proto.RequestMsg{
		Type:      proto.TypeRequest,
		RequestID: requestID,
		Model:     req.Model,
		Messages:  req.Messages,
		Stream:    false,
		Options:   buildOptions(req),
	}
	if err := donor.SendWS(requestMsg); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	// Wait for response with timeout.
	totalCtx, cancel := context.WithTimeout(ctx, proto.TotalRequestTimeout)
	defer cancel()

	select {
	case <-totalCtx.Done():
		_ = donor.SendWS(proto.CancelMsg{Type: proto.TypeCancel, RequestID: requestID})
		return nil, fmt.Errorf("timeout")

	case cr, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("donor disconnected")
		}
		if cr.Err != "" {
			return nil, fmt.Errorf("donor error: %s", cr.Err)
		}
		result := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]string{
						"role":    "assistant",
						"content": cr.Content,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     0,
				"completion_tokens": cr.Tokens,
				"total_tokens":      cr.Tokens,
			},
		}
		return result, nil
	}
}

func (s *Server) sendSSEDone(w http.ResponseWriter, flusher http.Flusher) {
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// buildOptions creates Ollama-compatible options from the chat completion request.
func buildOptions(req *proto.ChatCompletionRequest) json.RawMessage {
	opts := map[string]interface{}{}
	if req.Temperature != 0 {
		opts["temperature"] = req.Temperature
	}
	if req.TopP != 0 {
		opts["top_p"] = req.TopP
	}
	if req.MaxTokens != 0 {
		opts["num_predict"] = req.MaxTokens
	}
	if len(opts) == 0 {
		return nil
	}
	data, _ := json.Marshal(opts)
	return data
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func generateRequestID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// requireAPIKey is middleware that validates an API key from Authorization: Bearer header.
// It stores the key hash in the request context for downstream use.
func (s *Server) requireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			log.Printf("auth: no Authorization header from %s path=%s", r.RemoteAddr, r.URL.Path)
			writeError(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}
		if !strings.HasPrefix(authHeader, "Bearer ") {
			log.Printf("auth: malformed Authorization from %s path=%s prefix=%q", r.RemoteAddr, r.URL.Path, authHeader[:min(len(authHeader), 20)])
			writeError(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		keyHash := hashKey(token)
		key, err := s.store.FindKeyByHash(keyHash)
		if err != nil {
			log.Printf("auth: lookup error from %s: %v", r.RemoteAddr, err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if key == nil {
			log.Printf("auth: invalid key from %s path=%s token_prefix=%q len=%d hash=%s raw_auth=%q",
				r.RemoteAddr, r.URL.Path, token[:min(len(token), 12)], len(token), keyHash[:16], authHeader[:min(len(authHeader), 60)])
			writeError(w, http.StatusUnauthorized, "invalid API key")
			return
		}
		// Store the key hash for rate limiting.
		ctx := context.WithValue(r.Context(), ctxKeyAPIKeyHash, keyHash)
		ctx = context.WithValue(ctx, ctxKeyUserID, key.UserID)
		next(w, r.WithContext(ctx))
	}
}

// handleReport accepts an abuse report for a donor response (§5.1 SPEC).
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var report struct {
		RequestID string `json:"request_id"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if report.RequestID == "" {
		writeError(w, http.StatusBadRequest, "request_id is required")
		return
	}

	// For MVP, log the report. Future: store in DB, auto-flag donors.
	userID := getUserID(r)
	log.Printf("report: user_id=%d request_id=%s reason=%s", userID, report.RequestID, report.Reason)

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "ok"})
}
