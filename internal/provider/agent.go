package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/r00takaspin/gpumesh/internal/proto"
)

// Config holds the provider agent configuration (§4.2 SPEC).
type Config struct {
	CoordinatorURL  string
	Token           string
	OllamaURL       string
	Models          []string // empty = auto-discover all
	MaxConcurrent   int
	Description     string
	ReconnectMin    time.Duration
	ReconnectMax    time.Duration
}

// Agent is the provider agent that connects to the coordinator and proxies backend requests.
type Agent struct {
	cfg    Config
	conn   *websocket.Conn
	mu     sync.Mutex

	currentLoad int
	// Active requests: request_id → cancel function
	requests map[string]context.CancelFunc
	reqMu    sync.Mutex

	providerID  string
	done        chan struct{}
	backendType BackendType // Ollama or OpenAI-compatible API

	writeMu    sync.Mutex // serialises writes to conn (gorilla/websocket not concurrent-safe)
	httpClient *http.Client
}

// writeWS serialises a JSON message to the coordinator over the WebSocket connection.
// gorilla/websocket requires at most one concurrent writer.
func (a *Agent) writeWS(conn *websocket.Conn, v interface{}) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return conn.WriteJSON(v)
}


// autoDetectOllama probes for a running Ollama instance.
// Checks OLLAMA_HOST env var first, then probes localhost:11434 with a 2s timeout.
// Falls back to cfgURL if nothing is reachable.
func autoDetectOllama(ctx context.Context, cfgURL string) string {
	if host := os.Getenv("OLLAMA_HOST"); host != "" {
		if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
			host = "http://" + host
		}
		return host
	}
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://localhost:11434/api/tags", nil)
	if err != nil {
		return cfgURL
	}
	if resp, err := client.Do(req); err == nil {
		_ = resp.Body.Close()
		return "http://localhost:11434"
	}
	return cfgURL
}
// NewAgent creates a new provider agent.
func NewAgent(cfg Config) *Agent {
	if cfg.OllamaURL == "" {
		cfg.OllamaURL = autoDetectOllama(context.Background(), "http://localhost:11434")
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 1
	}
	if cfg.ReconnectMin == 0 {
		cfg.ReconnectMin = 1 * time.Second
	}
	if cfg.ReconnectMax == 0 {
		cfg.ReconnectMax = 60 * time.Second
	}
	if cfg.Description == "" {
		host, _ := os.Hostname()
		cfg.Description = host
	}
	return &Agent{
		cfg:        cfg,
		requests:   make(map[string]context.CancelFunc),
		done:       make(chan struct{}),
		httpClient: &http.Client{Timeout: proto.TotalRequestTimeout},
	}
}

// Run connects to the coordinator and starts the request processing loop.
func (a *Agent) Run(ctx context.Context) error {
	if a.cfg.Token == "" {
		return fmt.Errorf("no token provided. Get a provider token at /share")
	}

	backoff := a.cfg.ReconnectMin
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := a.connect(ctx); err != nil {
			log.Printf("\033[31m✗\033[0m connection error: %v", err)
		} else {
			// readLoop returned (clean disconnect). Reset backoff and retry immediately.
			backoff = a.cfg.ReconnectMin
			continue
		}

		// Wait with backoff before retrying.
		log.Printf("\033[33m⟳\033[0m retrying in %v...", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		// Exponential backoff with jitter, capped at ReconnectMax.
		jitter := time.Duration(rand.Int64N(int64(backoff) / 4))
		backoff = backoff*2 + jitter
		if backoff > a.cfg.ReconnectMax {
			backoff = a.cfg.ReconnectMax
		}
	}
}

func (a *Agent) connect(ctx context.Context) error {
	url := a.cfg.CoordinatorURL + "?token=" + a.cfg.Token
	log.Printf("\033[36m⌬\033[0m connecting to %s", a.cfg.CoordinatorURL)

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
			_ = resp.Body.Close()
			return fmt.Errorf("dial: %w (status=%d body=%q)", err, resp.StatusCode, truncateForLog(string(body), 200))
		}
		return fmt.Errorf("dial: %w", err)
	}

	a.mu.Lock()
	a.conn = conn
	a.mu.Unlock()

	// Discover models.
	models, err := a.discoverModels()
	if err != nil {
		log.Printf("model discovery error: %v", err)
		models = a.cfg.Models
	}
	if len(models) == 0 {
		_ = conn.Close()
		return fmt.Errorf("no models available")
	}

	log.Printf("\033[33m⬡\033[0m models: %v", models)

	// Send register.
	if err := conn.WriteJSON(proto.RegisterMsg{
		Type:          proto.TypeRegister,
		Models:        models,
		MaxConcurrent: a.cfg.MaxConcurrent,
		Description:   a.cfg.Description,
		Hardware:      detectHardware(),
	}); err != nil {
		_ = conn.Close()
		return fmt.Errorf("register: %w", err)
	}

	// Read registered response.
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("read registered: %w", err)
	}

	var reg proto.RegisteredMsg
	if err := json.Unmarshal(raw, &reg); err != nil || reg.Type != proto.TypeRegistered {
		_ = conn.Close()
		return fmt.Errorf("unexpected registration response: %s", raw)
	}

	a.providerID = reg.ProviderID
	if reg.MachineID != "" {
		log.Printf("\033[32m⚡\033[0m \033[1mregistered\033[0m machine_id=%s session_id=%s", reg.MachineID, a.providerID)
	} else {
		log.Printf("\033[32m⚡\033[0m \033[1mregistered\033[0m provider_id=%s", a.providerID)
	}

	// Start heartbeat loop.
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	go a.heartbeatLoop(heartbeatCtx)

	// Read loop.
	return a.readLoop(ctx)
}
func (a *Agent) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(proto.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.mu.Lock()
			conn := a.conn
			a.mu.Unlock()
			if conn == nil {
				return
			}
			if err := a.writeWS(conn, proto.HeartbeatMsg{Type: proto.TypeHeartbeat}); err != nil {
				log.Printf("heartbeat write error, closing: %v", err)
				_ = conn.Close()
				a.mu.Lock()
				if a.conn == conn {
					a.conn = nil
				}
				a.mu.Unlock()
				return
			}
		}
	}
}

// readLoop reads messages from the coordinator WebSocket.
func (a *Agent) readLoop(ctx context.Context) error {
	defer func() {
		a.mu.Lock()
		if a.conn != nil {
			_ = a.conn.Close()
			a.conn = nil
		}
		a.mu.Unlock()
	}()

	// Close connection on context cancellation (Ctrl+C).
	go func() {
		<-ctx.Done()
		a.mu.Lock()
		if a.conn != nil {
			_ = a.conn.Close()
		}
		a.mu.Unlock()
	}()

	for {
		a.mu.Lock()
		conn := a.conn
		a.mu.Unlock()
		if conn == nil {
			return nil
		}

		_ = conn.SetReadDeadline(time.Now().Add(proto.HeartbeatTimeout))

		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		var env proto.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			log.Printf("invalid message: %v", err)
			continue
		}

		switch env.Type {
		case proto.TypeRequest:
			var msg proto.RequestMsg
			if err := json.Unmarshal(raw, &msg); err != nil {
				log.Printf("invalid request: %v", err)
				continue
			}
			go a.handleRequest(ctx, msg)

		case proto.TypeCancel:
			var msg proto.CancelMsg
			if err := json.Unmarshal(raw, &msg); err != nil {
				log.Printf("invalid cancel: %v", err)
				continue
			}
			a.reqMu.Lock()
			if cancel, ok := a.requests[msg.RequestID]; ok {
				cancel()
				delete(a.requests, msg.RequestID)
			}
			a.reqMu.Unlock()

		case proto.TypeHeartbeatAck:
			// Nothing to do.

		default:
			log.Printf("unknown message type: %q", env.Type)
		}
	}
}

func (a *Agent) handleRequest(ctx context.Context, msg proto.RequestMsg) {
	// Check capacity.
	a.mu.Lock()
	if a.currentLoad >= a.cfg.MaxConcurrent {
		a.mu.Unlock()
		a.mu.Lock()
		conn := a.conn
		a.mu.Unlock()
		if conn != nil {
			_ = a.writeWS(conn, proto.ErrorMsg{
				Type:      proto.TypeError,
				RequestID: msg.RequestID,
				Code:      proto.ErrOverloaded,
				Message:   "donor at max capacity",
			})
		}
		return
	}
	a.currentLoad++
	a.mu.Unlock()
	log.Printf("handleRequest: id=%s model=%s stream=%v", msg.RequestID, msg.Model, msg.Stream)

	// Send to Ollama.

	defer func() {
		a.mu.Lock()
		a.currentLoad--
		a.mu.Unlock()
		a.reqMu.Lock()
		delete(a.requests, msg.RequestID)
		a.reqMu.Unlock()
	}()

	reqCtx, cancel := context.WithCancel(ctx)
	a.reqMu.Lock()
	a.requests[msg.RequestID] = cancel
	a.reqMu.Unlock()
	defer cancel()

	// Send to backend.
	resp, err := a.sendToBackend(reqCtx, msg)
	if err != nil {
		a.mu.Lock()
		conn := a.conn
		a.mu.Unlock()
		if conn != nil {
			code := proto.ErrInternal
			msgStr := err.Error()
			if reqCtx.Err() == context.Canceled {
				return // request cancelled, don't send error
			}
			_ = a.writeWS(conn, proto.ErrorMsg{
				Type:      proto.TypeError,
				RequestID: msg.RequestID,
				Code:      code,
				Message:   msgStr,
			})
		}
		return
	}
	defer func() { _ = resp.Body.Close() }()
	log.Printf("handleRequest: backend responded status=%d", resp.StatusCode)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		log.Printf("handleRequest: backend error body: %s", truncateForLog(string(errBody), 500))
		a.mu.Lock()
		conn := a.conn
		a.mu.Unlock()
		if conn != nil {
			_ = a.writeWS(conn, proto.ErrorMsg{
				Type:      proto.TypeError,
				RequestID: msg.RequestID,
				Code:      proto.ErrInternal,
				Message:   fmt.Sprintf("backend status %d: %s", resp.StatusCode, truncateForLog(string(errBody), 300)),
			})
		}
		return
	}

	if msg.Stream {
		a.handleStreamingResponse(msg.RequestID, resp.Body)
	} else {
		a.handleNonStreamingResponse(msg.RequestID, msg.Model, resp.Body)
	}
}

func (a *Agent) handleStreamingResponse(requestID string, body io.Reader) {
	scanner := bufio.NewScanner(body)

	for scanner.Scan() {
		line := scanner.Bytes()

		// OpenAI SSE format: skip empty lines and "data: [DONE]" sentinel.
		if a.backendType == BackendOpenAI {
			if len(line) == 0 {
				continue
			}
			if bytes.Equal(line, []byte("data: [DONE]")) {
				// Send final done chunk.
				a.mu.Lock()
				conn := a.conn
				a.mu.Unlock()
				if conn != nil {
					_ = a.writeWS(conn, proto.ChunkMsg{
						Type:      proto.TypeChunk,
						RequestID: requestID,
						Done:      true,
					})
				}
				return
			}
			// Strip "data: " prefix.
			const prefix = "data: "
			if bytes.HasPrefix(line, []byte(prefix)) {
				line = line[len(prefix):]
			} else {
				continue
			}
		}

		// Ollama format: empty lines skipped.
		if a.backendType != BackendOpenAI && len(line) == 0 {
			continue
		}

		var content string
		var thinking string
		var toolCalls json.RawMessage
		var done bool

		if a.backendType == BackendOpenAI {
			var openaiChunk struct {
				Choices []struct {
					Delta struct {
						Content   string          `json:"content"`
						ToolCalls json.RawMessage `json:"tool_calls"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal(line, &openaiChunk); err != nil {
				log.Printf("openai chunk parse error: %v", err)
				continue
			}
			if len(openaiChunk.Choices) > 0 {
				content = openaiChunk.Choices[0].Delta.Content
				toolCalls = openaiChunk.Choices[0].Delta.ToolCalls
				done = openaiChunk.Choices[0].FinishReason != nil
			}
		} else {
			var ollamaChunk struct {
				Message struct {
					Content   string          `json:"content"`
					Thinking  string          `json:"thinking"`
					ToolCalls json.RawMessage `json:"tool_calls"`
				} `json:"message"`
				Done bool `json:"done"`
			}
			if err := json.Unmarshal(line, &ollamaChunk); err != nil {
				log.Printf("ollama chunk parse error: %v", err)
				continue
			}
			content = ollamaChunk.Message.Content
			thinking = ollamaChunk.Message.Thinking
			toolCalls = ollamaChunk.Message.ToolCalls
			done = ollamaChunk.Done
		}

		a.mu.Lock()
		conn := a.conn
		a.mu.Unlock()
		if conn == nil {
			return
		}

		if content == "" && thinking != "" {
			content = thinking
		}
		normalizedToolCalls := normalizeToolCalls(toolCalls)
		if err := a.writeWS(conn, proto.ChunkMsg{
			Type:      proto.TypeChunk,
			RequestID: requestID,
			Content:   content,
			ToolCalls: normalizedToolCalls,
			Done:      done,
		}); err != nil {
			log.Printf("write chunk error: %v", err)
			return
		}

		if done {
			return
		}
	}
}
func (a *Agent) handleNonStreamingResponse(requestID, model string, body io.Reader) {
	data, err := io.ReadAll(body)
	if err != nil {
		log.Printf("read backend response error: %v", err)
		return
	}

	var content string
	var thinking string
	var toolCalls json.RawMessage
	var promptEval, eval int

	if a.backendType == BackendOpenAI {
		// OpenAI-compatible format: {"choices": [{"message": {"content": "...", "tool_calls": [...]}}], "usage": {...}}
		var openaiResp struct {
			Choices []struct {
				Message struct {
					Content   string          `json:"content"`
					ToolCalls json.RawMessage `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(data, &openaiResp); err != nil {
			log.Printf("openai response parse error: %v", err)
			return
		}
		if len(openaiResp.Choices) > 0 {
			content = openaiResp.Choices[0].Message.Content
			toolCalls = openaiResp.Choices[0].Message.ToolCalls
		}
		promptEval = openaiResp.Usage.PromptTokens
		eval = openaiResp.Usage.CompletionTokens
	} else {
		// Ollama format: {"message": {"content": "...", "tool_calls": [...]}}
		var ollamaResp struct {
			Message struct {
				Content   string          `json:"content"`
				Thinking  string          `json:"thinking"`
				ToolCalls json.RawMessage `json:"tool_calls"`
			} `json:"message"`
			TotalDuration   int64 `json:"total_duration"`
			PromptEvalCount int   `json:"prompt_eval_count"`
			EvalCount       int   `json:"eval_count"`
		}
		if err := json.Unmarshal(data, &ollamaResp); err != nil {
			log.Printf("ollama response parse error: %v", err)
			return
		}
		content = ollamaResp.Message.Content
		thinking = ollamaResp.Message.Thinking
		toolCalls = ollamaResp.Message.ToolCalls
		promptEval = ollamaResp.PromptEvalCount
		eval = ollamaResp.EvalCount
	}

	usage, _ := json.Marshal(map[string]interface{}{
		"prompt_tokens":     promptEval,
		"completion_tokens": eval,
		"total_tokens":      promptEval + eval,
	})

	a.mu.Lock()
	conn := a.conn
	a.mu.Unlock()
	if conn == nil {
		return
	}

	if content == "" && thinking != "" {
		content = thinking
	}
	normalizedToolCalls := normalizeToolCalls(toolCalls)
	if err := a.writeWS(conn, proto.ResponseMsg{
		Type:      proto.TypeResponse,
		RequestID: requestID,
		Content:   content,
		ToolCalls: normalizedToolCalls,
		Model:     model,
		Usage:     usage,
	}); err != nil {
		log.Printf("write response error: %v", err)
	} else {
		log.Printf("handleNonStreamingResponse: sent response_id=%s content_len=%d tool_calls=%v",
			requestID, len(content), len(normalizedToolCalls) > 0)
	}
}
func normalizeToolCalls(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var incoming []map[string]interface{}
	if err := json.Unmarshal(raw, &incoming); err != nil || len(incoming) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(incoming))
	for i, tc := range incoming {
		fn, _ := tc["function"].(map[string]interface{})
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		args := fn["arguments"]
		switch v := args.(type) {
		case map[string]interface{}, []interface{}:
			b, _ := json.Marshal(v)
			args = string(b)
		case nil:
			args = "{}"
		}
		id, _ := tc["id"].(string)
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		out = append(out, map[string]interface{}{
			"id":   id,
			"type": "function",
			"function": map[string]interface{}{
				"name":      name,
				"arguments": args,
			},
		})
	}
	if len(out) == 0 {
		return nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return b
}
// sendToBackend sends a chat completion request to the local backend (Ollama or OpenAI-compatible).
func (a *Agent) sendToBackend(ctx context.Context, msg proto.RequestMsg) (*http.Response, error) {
	messages, err := normalizeMessagesForOllama(msg.Messages)
	if err != nil {
		return nil, fmt.Errorf("normalize messages: %w", err)
	}

	ollamaReq := map[string]interface{}{
		"model":    msg.Model,
		"messages": messages,
		"stream":   msg.Stream,
	}
	if len(msg.Tools) > 0 {
		ollamaReq["tools"] = msg.Tools
	}
	if len(msg.ToolChoice) > 0 {
		ollamaReq["tool_choice"] = msg.ToolChoice
	}

	// Merge options.
	if len(msg.Options) > 0 {
		var opts map[string]interface{}
		if err := json.Unmarshal(msg.Options, &opts); err == nil {
			for k, v := range opts {
				ollamaReq[k] = v
			}
		}
	}

	body, err := json.Marshal(ollamaReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := "/api/chat"
	if a.backendType == BackendOpenAI {
		endpoint = "/v1/chat/completions"
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		a.cfg.OllamaURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	log.Printf("sendToBackend: calling %s%s model=%s", a.cfg.OllamaURL, endpoint, msg.Model)
	return a.httpClient.Do(httpReq)
}

// normalizeMessagesForOllama prepares OpenAI-shaped messages for Ollama /api/chat:
// - flattens content arrays (Cursor/Pi/Cline) to strings — else Ollama 400
//   "cannot unmarshal array into … content of type string"
// - parses tool_calls[].function.arguments JSON strings into objects — else Ollama 400
//   "Value looks like object, but can't find closing '}' symbol"
func normalizeMessagesForOllama(raw json.RawMessage) ([]map[string]interface{}, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty messages")
	}
	var msgs []map[string]interface{}
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(msgs))
	for _, m := range msgs {
		nm := make(map[string]interface{}, len(m))
		for k, v := range m {
			nm[k] = v
		}
		if c, ok := nm["content"]; ok {
			nm["content"] = flattenMessageContent(c)
		}
		if nm["content"] == nil {
			nm["content"] = ""
		}
		normalizeToolCallArgumentsInMessage(nm)
		out = append(out, nm)
	}
	return out, nil
}

func flattenMessageContent(content interface{}) interface{} {
	switch v := content.(type) {
	case string:
		return v
	case nil:
		return ""
	case []interface{}:
		var b strings.Builder
		for _, part := range v {
			pm, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			typ, _ := pm["type"].(string)
			switch typ {
			case "text", "":
				if t, ok := pm["text"].(string); ok {
					b.WriteString(t)
				}
			case "image_url":
				b.WriteString("[image]")
			default:
				// tool_result / other parts — keep any text field
				if t, ok := pm["text"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func truncateForLog(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func normalizeToolCallArgumentsInMessage(m map[string]interface{}) {
	if tcs, ok := m["tool_calls"].([]interface{}); ok {
		for _, tc := range tcs {
			tcm, ok := tc.(map[string]interface{})
			if !ok {
				continue
			}
			fn, _ := tcm["function"].(map[string]interface{})
			if fn == nil {
				continue
			}
			fn["arguments"] = ensureArgsObject(fn["arguments"])
		}
	}
	if fc, ok := m["function_call"].(map[string]interface{}); ok {
		fc["arguments"] = ensureArgsObject(fc["arguments"])
	}
}

func ensureArgsObject(args interface{}) interface{} {
	switch v := args.(type) {
	case map[string]interface{}:
		return v
	case nil:
		return map[string]interface{}{}
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return map[string]interface{}{}
		}
		var obj interface{}
		if err := json.Unmarshal([]byte(s), &obj); err != nil {
			return v
		}
		if obj == nil {
			return map[string]interface{}{}
		}
		return obj
	default:
		return v
	}
}

// discoverModels fetches available models from the backend.
// Uses FetchModels which tries Ollama /api/tags first, then OpenAI /v1/models.
// Also detects the backend type and stores it for later request routing.
func (a *Agent) discoverModels() ([]string, error) {
	allModels, backendType, err := FetchModels(a.cfg.OllamaURL)
	if err != nil {
		return nil, err
	}
	a.backendType = backendType

	// If whitelist configured, only include those.
	if len(a.cfg.Models) > 0 {
		wlSet := make(map[string]bool, len(a.cfg.Models))
		for _, wl := range a.cfg.Models {
			wlSet[wl] = true
		}
		var filtered []string
		for _, m := range allModels {
			if wlSet[m] {
				filtered = append(filtered, m)
			}
		}
		return filtered, nil
	}
	return allModels, nil
}
