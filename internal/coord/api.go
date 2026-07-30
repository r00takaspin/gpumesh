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

// --- GET /v1/models (discovery: owned + bindings) ---

func (s *Server) handleAPIModels(w http.ResponseWriter, r *http.Request) {
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

	userID := getUserID(r)
	accessible, err := s.store.ListAccessibleMachines(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := proto.ModelListResponse{
		Object: "list",
		Data:   make([]proto.ModelEntry, 0),
	}

	for _, bi := range accessible {
		sess := s.registry.GetSession(bi.MachineID)
		online := sess != nil && sess.BackendOK
		load := 0.0
		models := []string{}
		if sess != nil {
			load = sess.LoadFraction()
			models = sess.Models
		}
		if !online {
			// Offline machines still appear in discovery with online=false and empty models.
			resp.Data = append(resp.Data, proto.ModelEntry{
				ID:          "",
				Object:      "model",
				OwnedBy:     bi.MachineID,
				MachineID:   bi.MachineID,
				MachineName: bi.DisplayName,
				Online:      false,
				Load:        0,
			})
			// Skip empty id entries — only emit real models when online.
			resp.Data = resp.Data[:len(resp.Data)-1]
			continue
		}
		for _, name := range models {
			resp.Data = append(resp.Data, proto.ModelEntry{
				ID:          name,
				Object:      "model",
				OwnedBy:     bi.MachineID,
				MachineID:   bi.MachineID,
				MachineName: bi.DisplayName,
				Online:      true,
				Load:        load,
			})
		}
	}

	sort.Slice(resp.Data, func(i, j int) bool {
		if resp.Data[i].MachineID != resp.Data[j].MachineID {
			return resp.Data[i].MachineID < resp.Data[j].MachineID
		}
		return resp.Data[i].ID < resp.Data[j].ID
	})

	writeJSON(w, http.StatusOK, resp)
}

// --- GET /v1/machines/{machine_id}/models ---

func (s *Server) handleMachineModels(w http.ResponseWriter, r *http.Request) {
	machineID := r.PathValue("machine_id")
	userID := getUserID(r)

	ok, err := s.store.CanAccessMachine(userID, machineID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

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

	sess := s.registry.GetSession(machineID)
	resp := proto.ModelListResponse{Object: "list", Data: []proto.ModelEntry{}}
	if sess == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	load := sess.LoadFraction()
	online := sess.BackendOK
	for _, name := range sess.Models {
		resp.Data = append(resp.Data, proto.ModelEntry{
			ID:      name,
			Object:  "model",
			OwnedBy: "owner",
			Online:  online,
			Load:    load,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- POST /v1/machines/{machine_id}/chat/completions ---

func (s *Server) handleMachineChatCompletions(w http.ResponseWriter, r *http.Request) {
	machineID := r.PathValue("machine_id")
	userID := getUserID(r)

	ok, err := s.store.CanAccessMachine(userID, machineID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	keyHash, _ := r.Context().Value(ctxKeyAPIKeyHash).(string)
	allowed, remaining := s.limiter.Allow(keyHash)
	w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
	if !allowed {
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	var req proto.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}
	if len(req.Messages) == 0 || req.Messages[0] != '[' || string(req.Messages) == "[]" {
		writeError(w, http.StatusBadRequest, "messages must be a non-empty array")
		return
	}

	// Strip provider prefix (openai/llama3.2 → llama3.2).
	if idx := strings.IndexByte(req.Model, '/'); idx >= 0 {
		req.Model = req.Model[idx+1:]
	}

	log.Printf("chat: machine_id=%s model=%s stream=%v tools=%v msgs_bytes=%d ua=%q",
		machineID, req.Model, req.Stream, len(req.Tools) > 0, len(req.Messages), r.UserAgent())

	atomic.AddInt64(&s.requestsToday, 1)

	sess := s.registry.GetSession(machineID)
	if sess == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "machine_offline",
		})
		return
	}
	if !sess.BackendOK {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "backend_unavailable",
		})
		return
	}
	if sess.CurrentLoad >= sess.MaxConcurrent {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "machine_busy",
		})
		return
	}
	if !s.registry.HasModel(machineID, req.Model) {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "model_not_found",
		})
		return
	}

	requestID := generateRequestID()
	if req.Stream {
		s.handleStreamingCompletion(w, r, sess, &req, requestID)
	} else {
		s.handleNonStreamingCompletion(w, r, sess, &req, requestID)
	}
}

// handleLegacyChatCompletions returns 410 for v1 pool path.
func (s *Server) handleLegacyChatCompletions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusGone, map[string]string{
		"error": "gone",
		"message": "Use POST /v1/machines/{machine_id}/chat/completions — pool routing was removed in v2",
	})
}

func (s *Server) handleStreamingCompletion(w http.ResponseWriter, r *http.Request, sess *MachineSession, req *proto.ChatCompletionRequest, requestID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx proxy buffering for SSE
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("chat: streaming not supported")
		return
	}

	ch := make(chan ChunkRelay, 32)
	sess.RegisterChunkChannel(requestID, ch)
	defer sess.UnregisterChunkChannel(requestID)

	if !s.registry.IncrementLoad(sess.MachineID) {
		sess.UnregisterChunkChannel(requestID)
		_, _ = fmt.Fprintf(w, "data: {\"error\":\"machine_busy\"}\n\n")
		flusher.Flush()
		return
	}

	requestMsg := proto.RequestMsg{
		Type:       proto.TypeRequest,
		RequestID:  requestID,
		Model:      req.Model,
		Messages:   req.Messages,
		Stream:     true,
		Tools:      req.Tools,
		ToolChoice: req.ToolChoice,
		Options:    buildOptions(req),
	}
	if err := sess.SendWS(requestMsg); err != nil {
		log.Printf("chat: send request error: %v", err)
		s.registry.DecrementLoad(sess.MachineID)
		_, _ = fmt.Fprintf(w, "data: {\"error\":\"machine_offline\"}\n\n")
		flusher.Flush()
		return
	}

	totalCtx, totalCancel := context.WithTimeout(r.Context(), proto.TotalRequestTimeout)
	defer totalCancel()

	ttftTimer := time.NewTimer(proto.TTFTTimeout)
	defer ttftTimer.Stop()

	interTokenTimer := time.NewTimer(proto.InterTokenTimeout)
	interTokenTimer.Stop()
	var firstTokenReceived bool
	var sawToolCalls bool
	created := time.Now().Unix()

	for {
		select {
		case <-totalCtx.Done():
			_ = sess.SendWS(proto.CancelMsg{Type: proto.TypeCancel, RequestID: requestID})
			if firstTokenReceived {
				finalChunk := map[string]interface{}{
					"id":      "chatcmpl-" + requestID,
					"object":  "chat.completion.chunk",
					"created": created,
					"model":   req.Model,
					"choices": []map[string]interface{}{
						{"index": 0, "delta": map[string]string{}, "finish_reason": "length"},
					},
				}
				data, _ := json.Marshal(finalChunk)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
			s.sendSSEDone(w, flusher)
			s.registry.DecrementLoad(sess.MachineID)
			return

		case <-r.Context().Done():
			_ = sess.SendWS(proto.CancelMsg{Type: proto.TypeCancel, RequestID: requestID})
			s.registry.DecrementLoad(sess.MachineID)
			return

		case <-ttftTimer.C:
			_ = sess.SendWS(proto.CancelMsg{Type: proto.TypeCancel, RequestID: requestID})
			_, _ = fmt.Fprintf(w, "data: {\"error\":\"timeout waiting for first token\"}\n\n")
			flusher.Flush()
			s.registry.DecrementLoad(sess.MachineID)
			return

		case <-interTokenTimer.C:
			_ = sess.SendWS(proto.CancelMsg{Type: proto.TypeCancel, RequestID: requestID})
			_, _ = fmt.Fprintf(w, "data: {\"error\":\"inter-token timeout\"}\n\n")
			flusher.Flush()
			s.registry.DecrementLoad(sess.MachineID)
			return

		case cr, ok := <-ch:
			if !ok {
				_, _ = fmt.Fprintf(w, "data: {\"error\":\"machine_offline\"}\n\n")
				flusher.Flush()
				s.registry.DecrementLoad(sess.MachineID)
				return
			}
			if cr.Err != "" {
				_, _ = fmt.Fprintf(w, "data: {\"error\":%q}\n\n", cr.Err)
				flusher.Flush()
				s.registry.DecrementLoad(sess.MachineID)
				return
			}
			if !firstTokenReceived {
				firstTokenReceived = true
				ttftTimer.Stop()
				interTokenTimer.Reset(proto.InterTokenTimeout)
			} else {
				if !interTokenTimer.Stop() {
					select {
					case <-interTokenTimer.C:
					default:
					}
				}
				interTokenTimer.Reset(proto.InterTokenTimeout)
			}

			if len(cr.ToolCalls) > 0 {
				sawToolCalls = true
			}
			// Skip empty keepalives (no content / tool_calls) — Cursor treats long empty SSE as hang.
			if !cr.Done && cr.Content == "" && len(cr.ToolCalls) == 0 {
				continue
			}

			delta := map[string]interface{}{}
			if cr.Content != "" {
				delta["content"] = cr.Content
			}
			if len(cr.ToolCalls) > 0 {
				var toolCalls interface{}
				if err := json.Unmarshal(cr.ToolCalls, &toolCalls); err == nil {
					delta["tool_calls"] = normalizeStreamToolCallDeltas(toolCalls)
				}
			}
			var finish interface{}
			if cr.Done {
				if sawToolCalls {
					finish = "tool_calls"
				} else {
					finish = "stop"
				}
			}
			chunk := map[string]interface{}{
				"id":      "chatcmpl-" + requestID,
				"object":  "chat.completion.chunk",
				"created": created,
				"model":   req.Model,
				"choices": []map[string]interface{}{
					{
						"index":         0,
						"delta":         delta,
						"finish_reason": finish,
					},
				},
			}
			data, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

			if cr.Done {
				_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				s.registry.DecrementLoad(sess.MachineID)
				return
			}
		}
	}
}

// handleNonStreamingCompletion retries up to MaxRetries on the same machine only (§8).
func (s *Server) handleNonStreamingCompletion(w http.ResponseWriter, r *http.Request, sess *MachineSession, req *proto.ChatCompletionRequest, requestID string) {
	machineID := sess.MachineID
	for attempt := range proto.MaxRetries {
		if attempt > 0 {
			sess = s.registry.GetSession(machineID)
			if sess == nil || !sess.BackendOK {
				break
			}
			if sess.CurrentLoad >= sess.MaxConcurrent {
				break
			}
			requestID = generateRequestID()
		}

		result, err := s.sendNonStreamingRequest(r.Context(), sess, req, requestID)
		if err == nil {
			result["id"] = "chatcmpl-" + requestID
			result["object"] = "chat.completion"
			result["created"] = time.Now().Unix()
			result["model"] = req.Model
			writeJSON(w, http.StatusOK, result)
			return
		}
		log.Printf("chat: non-streaming attempt %d/%d machine_id=%s failed: %v",
			attempt+1, proto.MaxRetries, machineID, err)
	}

	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "machine_failed"})
}

func (s *Server) sendNonStreamingRequest(ctx context.Context, sess *MachineSession, req *proto.ChatCompletionRequest, requestID string) (map[string]interface{}, error) {
	if !s.registry.IncrementLoad(sess.MachineID) {
		return nil, fmt.Errorf("machine_busy")
	}
	defer s.registry.DecrementLoad(sess.MachineID)

	ch := make(chan ChunkRelay, 1)
	sess.RegisterChunkChannel(requestID, ch)
	defer sess.UnregisterChunkChannel(requestID)

	requestMsg := proto.RequestMsg{
		Type:       proto.TypeRequest,
		RequestID:  requestID,
		Model:      req.Model,
		Messages:   req.Messages,
		Stream:     false,
		Tools:      req.Tools,
		ToolChoice: req.ToolChoice,
		Options:    buildOptions(req),
	}
	if err := sess.SendWS(requestMsg); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	totalCtx, cancel := context.WithTimeout(ctx, proto.TotalRequestTimeout)
	defer cancel()

	select {
	case <-totalCtx.Done():
		_ = sess.SendWS(proto.CancelMsg{Type: proto.TypeCancel, RequestID: requestID})
		return nil, fmt.Errorf("timeout")

	case cr, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("machine_offline")
		}
		if cr.Err != "" {
			return nil, fmt.Errorf("provider error: %s", cr.Err)
		}
		message := map[string]interface{}{
			"role":    "assistant",
			"content": cr.Content,
		}
		finish := "stop"
		if len(cr.ToolCalls) > 0 {
			var toolCalls interface{}
			if err := json.Unmarshal(cr.ToolCalls, &toolCalls); err == nil {
				message["tool_calls"] = toolCalls
				finish = "tool_calls"
			}
		}
		return map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"message":       message,
					"finish_reason": finish,
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     0,
				"completion_tokens": cr.Tokens,
				"total_tokens":      cr.Tokens,
			},
		}, nil
	}
}

func (s *Server) sendSSEDone(w http.ResponseWriter, flusher http.Flusher) {
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// normalizeStreamToolCallDeltas ensures OpenAI streaming deltas include index.
func normalizeStreamToolCallDeltas(toolCalls interface{}) interface{} {
	arr, ok := toolCalls.([]interface{})
	if !ok {
		return toolCalls
	}
	for i, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if _, has := m["index"]; !has {
			m["index"] = i
		}
		arr[i] = m
	}
	return arr
}

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
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) requireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}
		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		keyHash := hashKey(token)
		key, err := s.store.FindKeyByHash(keyHash)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if key == nil {
			writeError(w, http.StatusUnauthorized, "invalid API key")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyAPIKeyHash, keyHash)
		ctx = context.WithValue(ctx, ctxKeyUserID, key.UserID)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var report struct {
		RequestID string `json:"request_id"`
		Reason    string `json:"reason"`
		MachineID string `json:"machine_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if report.RequestID == "" {
		writeError(w, http.StatusBadRequest, "request_id is required")
		return
	}
	if report.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}

	userID := getUserID(r)
	log.Printf("report: user_id=%d request_id=%s machine_id=%s reason=%s",
		userID, report.RequestID, report.MachineID, report.Reason)

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "ok"})
}
