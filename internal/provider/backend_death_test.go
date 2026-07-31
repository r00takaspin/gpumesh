package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/r00takaspin/gpumesh/internal/proto"
)

// TestBackendDeathErrors tests that when the backend (llama.cpp) is unreachable,
// the provider sends a proper ErrorMsg to the coordinator with:
//   - type: "error"
//   - code: "internal"
//   - message: connection refused (or similar dial error)
func TestBackendDeathErrors(t *testing.T) {
	// ── 1. Mock coordinator WebSocket server ─────────────────
	msgCh := make(chan []byte, 1)

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		// Coordinator sends a RequestMsg to the provider.
		req := proto.RequestMsg{
			Type:      proto.TypeRequest,
			RequestID: "req-dead-backend-01",
			Model:     "gpt-oss-20b",
			Messages:  json.RawMessage(`[{"role":"user","content":"hello"}]`),
			Stream:    false,
		}
		reqJSON, _ := json.Marshal(req)
		if err := conn.WriteMessage(websocket.TextMessage, reqJSON); err != nil {
			return
		}

		// Coordinator reads the provider's response (should be an ErrorMsg).
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		msgCh <- raw

		// Read one more message to drain (heartbeat or close).
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.ReadMessage()
	}))
	defer srv.Close()

	// ── 2. Create provider agent with DEAD backend ───────────
	// Use a URL that will always fail — nothing listening on this port.
	deadBackend := "http://127.0.0.1:19999"

	agent := NewAgent(Config{
		CoordinatorURL: "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/provider",
		OllamaURL:      deadBackend,
		Token:          "test-token",
		Models:         []string{"gpt-oss-20b"},
		ReconnectMin:   10 * time.Second, // don't reconnect during test
		ReconnectMax:   10 * time.Second,
	})
	// Set a short HTTP client timeout so the test doesn't hang.
	agent.httpClient.Timeout = 2 * time.Second

	// ── 3. Connect to mock coordinator ───────────────────────
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/provider"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial coordinator: %v", err)
	}

	agent.mu.Lock()
	agent.conn = conn
	agent.mu.Unlock()

	// Start readLoop in background — it will read the request and call handleRequest.
	go agent.readLoop(context.Background())

	// ── 4. Wait for error response ───────────────────────────
	select {
	case raw := <-msgCh:
		var errMsg proto.ErrorMsg
		if err := json.Unmarshal(raw, &errMsg); err != nil {
			t.Fatalf("unmarshal error response: %v\nraw: %s", err, raw)
		}

		// ── ASSERTIONS ───────────────────────────────────
		if errMsg.Type != proto.TypeError {
			t.Errorf("type = %q, want %q", errMsg.Type, proto.TypeError)
		}
		if errMsg.RequestID != "req-dead-backend-01" {
			t.Errorf("request_id = %q, want %q", errMsg.RequestID, "req-dead-backend-01")
		}
		if errMsg.Code != proto.ErrBackendUnavailable {
			t.Errorf("code = %q, want %q (ErrBackendUnavailable)", errMsg.Code, proto.ErrBackendUnavailable)
		}

		// The message should contain a connection/dial error.
		msgLower := strings.ToLower(errMsg.Message)
		hasConnErr := strings.Contains(msgLower, "connection refused") ||
			strings.Contains(msgLower, "connect") ||
			strings.Contains(msgLower, "dial") ||
			strings.Contains(msgLower, "no such host")
		if !hasConnErr {
			t.Errorf("message = %q, expected connection/dial error", errMsg.Message)
		}

		t.Logf("✓ Backend death → correct ErrorMsg returned:")
		t.Logf("  type: %s", errMsg.Type)
		t.Logf("  code: %s", errMsg.Code)
		t.Logf("  request_id: %s", errMsg.RequestID)
		t.Logf("  message: %s", errMsg.Message)

	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for error response")
	}
}

// TestBackendDeathPreservesRequestID verifies that when the backend dies,
// the ErrorMsg preserves the original request ID so the coordinator can
// route the error to the correct client.
func TestBackendDeathPreservesRequestID(t *testing.T) {
	requests := []string{"req-001", "req-002", "req-003"}

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		// Send all three requests, collect all three error responses.
		for _, reqID := range requests {
			req := proto.RequestMsg{
				Type:      proto.TypeRequest,
				RequestID: reqID,
				Model:     "gpt-oss-20b",
				Messages:  json.RawMessage(`[{"role":"user","content":"hi"}]`),
				Stream:    false,
			}
			reqJSON, _ := json.Marshal(req)
			if err := conn.WriteMessage(websocket.TextMessage, reqJSON); err != nil {
				return
			}
		}

		// Read three error responses — order is non-deterministic
		// because handleRequest runs in goroutines.
		seen := make(map[string]bool)
		for range 3 {
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var errMsg proto.ErrorMsg
			if err := json.Unmarshal(raw, &errMsg); err != nil {
				continue
			}
			if errMsg.Code != proto.ErrBackendUnavailable {
				t.Errorf("code = %q, want %q", errMsg.Code, proto.ErrBackendUnavailable)
			}
			seen[errMsg.RequestID] = true
		}
		for _, reqID := range requests {
			if !seen[reqID] {
				t.Errorf("missing response for request_id %q", reqID)
			}
		}
	}))
	defer srv.Close()

	agent := NewAgent(Config{
		CoordinatorURL: "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/provider",
		OllamaURL:      "http://127.0.0.1:19999", // dead
		Token:          "test-token",
		Models:         []string{"gpt-oss-20b"},
		MaxConcurrent:   3,
		ReconnectMin:   10 * time.Second,
		ReconnectMax:   10 * time.Second,
	})
	agent.httpClient.Timeout = 2 * time.Second

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/provider"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	agent.mu.Lock()
	agent.conn = conn
	agent.mu.Unlock()

	// readLoop with context that cancels after a timeout.
	done := make(chan struct{})
	go func() {
		agent.readLoop(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout")
	}
}

// TestBackendHTTPError tests that a non-2xx backend response is properly
// forwarded as an ErrorMsg with the backend status code in the message.
func TestBackendHTTPError(t *testing.T) {
	// Mock backend that always returns 500.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"GPU OOM"}`))
	}))
	defer backend.Close()

	upgrader := websocket.Upgrader{}
	var capturedErr proto.ErrorMsg

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		req := proto.RequestMsg{
			Type:      proto.TypeRequest,
			RequestID: "req-500-test",
			Model:     "gpt-oss-20b",
			Messages:  json.RawMessage(`[{"role":"user","content":"hi"}]`),
			Stream:    false,
		}
		reqJSON, _ := json.Marshal(req)
		conn.WriteMessage(websocket.TextMessage, reqJSON)

		_, raw, _ := conn.ReadMessage()
		json.Unmarshal(raw, &capturedErr)
	}))
	defer srv.Close()

	agent := NewAgent(Config{
		CoordinatorURL: "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/provider",
		OllamaURL:      backend.URL,
		Token:          "test-token",
		Models:         []string{"gpt-oss-20b"},
		ReconnectMin:   10 * time.Second,
		ReconnectMax:   10 * time.Second,
	})

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/provider"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	agent.mu.Lock()
	agent.conn = conn
	agent.mu.Unlock()

	go agent.readLoop(context.Background())

	// Wait for error to be captured.
	time.Sleep(1 * time.Second)

	if capturedErr.Code != proto.ErrInternal {
		t.Errorf("code = %q, want %q", capturedErr.Code, proto.ErrInternal)
	}
	if !strings.Contains(capturedErr.Message, "500") {
		t.Errorf("message = %q, expected to contain '500'", capturedErr.Message)
	}
	if !strings.Contains(capturedErr.Message, "GPU OOM") {
		t.Errorf("message = %q, expected to contain 'GPU OOM'", capturedErr.Message)
	}

	t.Logf("✓ Backend HTTP 500 → ErrorMsg: %s", capturedErr.Message)
}
