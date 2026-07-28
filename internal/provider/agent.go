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
	"net"
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

// Agent is the donor agent that connects to the coordinator and proxies Ollama requests.
type Agent struct {
	cfg    Config
	conn   *websocket.Conn
	mu     sync.Mutex

	currentLoad int
	// Active requests: request_id → cancel function
	requests map[string]context.CancelFunc
	reqMu    sync.Mutex

	providerID string
	done       chan struct{}

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
		return fmt.Errorf("no token provided. Get one at https://gpumesh.net/dashboard")
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

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{KeepAlive: 30 * time.Second}
			conn, err := d.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetKeepAlivePeriod(30 * time.Second)
			}
			return conn, nil
		},
	}
	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
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
	log.Printf("\033[32m⚡\033[0m \033[1mregistered\033[0m provider_id=%s", a.providerID)

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

	// Send to Ollama.
	ollamaResp, err := a.sendToOllama(reqCtx, msg)
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
	defer func() { _ = ollamaResp.Body.Close() }()
	log.Printf("handleRequest: ollama responded status=%d", ollamaResp.StatusCode)

	if msg.Stream {
		a.handleStreamingResponse(msg.RequestID, ollamaResp.Body)
	} else {
		a.handleNonStreamingResponse(msg.RequestID, msg.Model, ollamaResp.Body)
	}
}

func (a *Agent) handleStreamingResponse(requestID string, body io.Reader) {
	scanner := bufio.NewScanner(body)
	// Ollama returns NDJSON lines.

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
		}
		if err := json.Unmarshal(line, &chunk); err != nil {
			log.Printf("ollama chunk parse error: %v", err)
			continue
		}

		a.mu.Lock()
		conn := a.conn
		a.mu.Unlock()
		if conn == nil {
			return
		}

		if err := a.writeWS(conn, proto.ChunkMsg{
			Type:      proto.TypeChunk,
			RequestID: requestID,
			Content:   chunk.Message.Content,
			Done:      chunk.Done,
		}); err != nil {
			log.Printf("write chunk error: %v", err)
			return
		}

		if chunk.Done {
			return
		}
	}
}

func (a *Agent) handleNonStreamingResponse(requestID, model string, body io.Reader) {
	data, err := io.ReadAll(body)
	if err != nil {
		log.Printf("read ollama response error: %v", err)
		return
	}

	var ollamaResp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		TotalDuration      int64 `json:"total_duration"`
		PromptEvalCount    int   `json:"prompt_eval_count"`
		EvalCount          int   `json:"eval_count"`
	}
	if err := json.Unmarshal(data, &ollamaResp); err != nil {
		log.Printf("ollama response parse error: %v", err)
		return
	}

	usage, _ := json.Marshal(map[string]interface{}{
		"prompt_tokens":     ollamaResp.PromptEvalCount,
		"completion_tokens": ollamaResp.EvalCount,
		"total_tokens":      ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
	})

	a.mu.Lock()
	conn := a.conn
	a.mu.Unlock()
	if conn == nil {
		return
	}

	if err := a.writeWS(conn, proto.ResponseMsg{
		Type:      proto.TypeResponse,
		RequestID: requestID,
		Content:   ollamaResp.Message.Content,
		Model:     model,
		Usage:     usage,
	}); err != nil {
		log.Printf("write response error: %v", err)
	} else {
		log.Printf("handleNonStreamingResponse: sent response_id=%s content_len=%d", requestID, len(ollamaResp.Message.Content))
	}
}

// sendToOllama sends a chat completion request to the local Ollama instance.
func (a *Agent) sendToOllama(ctx context.Context, msg proto.RequestMsg) (*http.Response, error) {
	ollamaReq := map[string]interface{}{
		"model":    msg.Model,
		"messages": msg.Messages,
		"stream":   msg.Stream,
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
		return nil, fmt.Errorf("marshal ollama request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		a.cfg.OllamaURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	log.Printf("sendToOllama: calling %s/api/chat model=%s", a.cfg.OllamaURL, msg.Model)
	return a.httpClient.Do(httpReq)
}

// discoverModels fetches available models from Ollama.
func (a *Agent) discoverModels() ([]string, error) {
	resp, err := http.Get(a.cfg.OllamaURL + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("ollama /api/tags: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse /api/tags: %w", err)
	}

	var models []string
	for _, m := range result.Models {
		// If whitelist configured, only include those.
		if len(a.cfg.Models) > 0 {
			for _, wl := range a.cfg.Models {
				if wl == m.Name {
					models = append(models, m.Name)
					break
				}
			}
		} else {
			models = append(models, m.Name)
		}
	}
	return models, nil
}

