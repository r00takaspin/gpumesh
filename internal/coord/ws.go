package coord

import (
	"crypto/rand"
	"sync/atomic"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/r00takaspin/gpumesh/internal/proto"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleWSProvider handles WebSocket connections from provider agents.
func (s *Server) handleWSProvider(w http.ResponseWriter, r *http.Request) {
	// Authenticate: token from query parameter.
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	hash := hashKey(token)
	key, err := s.store.FindKeyByHash(hash)
	if err != nil {
		log.Printf("ws auth error: %v", err)
		http.Error(w, "auth error", http.StatusInternalServerError)
		return
	}
	if key == nil {
		http.Error(w, "invalid or revoked token", http.StatusUnauthorized)
		return
	}
	if key.Scope != proto.ScopeDonor && key.Scope != proto.ScopeBoth {
		http.Error(w, "token scope does not allow donor access", http.StatusForbidden)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}

	providerID := generateProviderID()
	log.Printf("ws: donor connected provider_id=%s user_id=%d", providerID, key.UserID)

	donor := &Donor{
		ProviderID:  providerID,
		UserID:      key.UserID,
		WSConn:      conn,
		Description: "",
	}

	// Read initial register message.
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		log.Printf("ws: read register error: %v", err)
		conn.Close()
		return
	}

	var env proto.Envelope
	if err := json.Unmarshal(raw, &env); err != nil || env.Type != proto.TypeRegister {
		log.Printf("ws: expected register, got type=%q err=%v", env.Type, err)
		conn.Close()
		return
	}

	var reg proto.RegisterMsg
	if err := json.Unmarshal(raw, &reg); err != nil {
		log.Printf("ws: invalid register: %v", err)
		conn.Close()
		return
	}

	donor.Models = reg.Models
	donor.MaxConcurrent = reg.MaxConcurrent
	donor.Description = reg.Description
	if donor.MaxConcurrent <= 0 {
		donor.MaxConcurrent = 1
	}

	// Add to registry.
	s.registry.Register(donor)

	// Send registered response.
	if err := donor.SendWS(proto.RegisteredMsg{
		Type:       proto.TypeRegistered,
		ProviderID: providerID,
	}); err != nil {
		log.Printf("ws: send registered error: %v", err)
		s.registry.Unregister(providerID)
		conn.Close()
		return
	}

	// Disable read deadline for message loop.
	conn.SetReadDeadline(time.Time{})
	// Read loop in a goroutine; wait for it to finish.
	readDone := make(chan struct{})
	go func() {
		s.readLoop(donor)
		close(readDone)
	}()

	<-readDone

	// Cleanup.
	userID, sessionReqs, sessionTokens := s.registry.Unregister(providerID)
	log.Printf("ws: donor disconnected provider_id=%s requests=%d tokens=%d",
		providerID, sessionReqs, sessionTokens)

	// Persist session stats (best-effort).
	if userID > 0 && (sessionReqs > 0 || sessionTokens > 0) {
		uptimeSec := int64(time.Since(donor.ConnectedAt).Seconds())
		if err := s.store.UpdateDonorStats(userID, int64(sessionReqs), int64(sessionTokens), uptimeSec); err != nil {
			log.Printf("ws: update donor stats error: %v", err)
		}
	}
}

func (s *Server) readLoop(donor *Donor) {
	defer donor.WSConn.Close()

	for {
		_, raw, err := donor.WSConn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("ws: read error provider_id=%s: %v", donor.ProviderID, err)
			}
			return
		}

		var env proto.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			log.Printf("ws: invalid message from %s: %v", donor.ProviderID, err)
			continue
		}

		switch env.Type {
		case proto.TypeHeartbeat:
			s.registry.UpdateHeartbeat(donor.ProviderID)
			// Send ack (best-effort).
			donor.SendWS(proto.HeartbeatAckMsg{Type: proto.TypeHeartbeatAck})

		case proto.TypeChunk:
			var msg proto.ChunkMsg
			if err := json.Unmarshal(raw, &msg); err != nil {
				log.Printf("ws: invalid chunk: %v", err)
				continue
			}
			s.relayChunk(donor.ProviderID, msg)
		case proto.TypeResponse:
			var msg proto.ResponseMsg
			if err := json.Unmarshal(raw, &msg); err != nil {
				log.Printf("ws: invalid response: %v", err)
				continue
			}
			log.Printf("ws: received response provider_id=%s request_id=%s", donor.ProviderID, msg.RequestID)
			s.relayResponse(donor.ProviderID, msg)

		case proto.TypeError:
			var msg proto.ErrorMsg
			if err := json.Unmarshal(raw, &msg); err != nil {
				log.Printf("ws: invalid error: %v", err)
				continue
			}
			s.relayError(donor.ProviderID, msg)

			// If backend_unavailable, mark donor.
			if msg.Code == proto.ErrBackendUnavailable {
				s.registry.SetBackendOK(donor.ProviderID, false)
			}

		default:
			log.Printf("ws: unknown message type %q from %s", env.Type, donor.ProviderID)
		}
	}
}

func (s *Server) relayChunk(providerID string, msg proto.ChunkMsg) {
	// Find the pending request and forward the chunk.
	// The chunk relay channels are indexed by request_id.
	donor := s.registry.GetDonor(providerID)
	if donor == nil {
		return
	}
	donor.mu.Lock()
	ch, ok := donor.chunkCh[msg.RequestID]
	donor.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- ChunkRelay{Content: msg.Content, Done: msg.Done}:
	default:
		// Consumer disconnected or channel full; drop chunk.
	}
	if msg.Done {
		donor.UnregisterChunkChannel(msg.RequestID)
	}
	// Count each chunk as one token for session stats.
	s.registry.AddTokens(providerID, 1)
	atomic.AddInt64(&s.tokensToday, 1)
	s.store.UpdateDonorStats(donor.UserID, 0, 1, 0)
}

func (s *Server) relayResponse(providerID string, msg proto.ResponseMsg) {
	donor := s.registry.GetDonor(providerID)
	if donor == nil {
		log.Printf("relayResponse: donor not found provider_id=%s request_id=%s", providerID, msg.RequestID)
		return
	}

	// Parse usage for token counting.
	var usage struct {
		CompletionTokens int `json:"completion_tokens"`
	}
	if len(msg.Usage) > 0 {
		json.Unmarshal(msg.Usage, &usage)
	}

	donor.mu.Lock()
	ch, ok := donor.chunkCh[msg.RequestID]
	donor.mu.Unlock()
	if !ok {
		log.Printf("relayResponse: no chunk channel for request_id=%s (have %d channels)", msg.RequestID, len(donor.chunkCh))
		return
	}
	select {
	case ch <- ChunkRelay{Content: msg.Content, Tokens: usage.CompletionTokens}:
		log.Printf("relayResponse: delivered request_id=%s content_len=%d tokens=%d", msg.RequestID, len(msg.Content), usage.CompletionTokens)
	default:
		log.Printf("relayResponse: channel full for request_id=%s", msg.RequestID)
	}
	donor.UnregisterChunkChannel(msg.RequestID)

	if usage.CompletionTokens > 0 {
		s.registry.AddTokens(providerID, usage.CompletionTokens)
		atomic.AddInt64(&s.tokensToday, int64(usage.CompletionTokens))
		s.store.UpdateDonorStats(donor.UserID, 1, int64(usage.CompletionTokens), 0)
	}
}
func (s *Server) relayError(providerID string, msg proto.ErrorMsg) {
	donor := s.registry.GetDonor(providerID)
	if donor == nil {
		return
	}
	donor.mu.Lock()
	ch, ok := donor.chunkCh[msg.RequestID]
	donor.mu.Unlock()
	if ok {
		select {
		case ch <- ChunkRelay{Err: msg.Code + ": " + msg.Message}:
		default:
		}
		donor.UnregisterChunkChannel(msg.RequestID)
	}

}

func generateProviderID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
