package coord

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/r00takaspin/gpumesh/internal/proto"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleWSProvider handles WebSocket connections from provider agents.
func (s *Server) handleWSProvider(w http.ResponseWriter, r *http.Request) {
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
	if !proto.IsProviderScope(key.Scope) {
		http.Error(w, "token scope does not allow provider access", http.StatusForbidden)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}

	sessionID := generateSessionID()

	// Read initial register message.
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		log.Printf("ws: read register error: %v", err)
		_ = conn.Close()
		return
	}

	var env proto.Envelope
	if err := json.Unmarshal(raw, &env); err != nil || env.Type != proto.TypeRegister {
		log.Printf("ws: expected register, got type=%q err=%v", env.Type, err)
		_ = conn.Close()
		return
	}

	var reg proto.RegisterMsg
	if err := json.Unmarshal(raw, &reg); err != nil {
		log.Printf("ws: invalid register: %v", err)
		_ = conn.Close()
		return
	}

	displayName := reg.Description
	machine, err := s.store.UpsertMachineByProviderKey(key.UserID, key.ID, displayName)
	if err != nil {
		log.Printf("ws: upsert machine error: %v", err)
		_ = conn.Close()
		return
	}

	log.Printf("ws: provider connected machine_id=%s session_id=%s user_id=%d",
		machine.ID, sessionID, key.UserID)

	sess := &MachineSession{
		MachineID:   machine.ID,
		SessionID:   sessionID,
		UserID:      key.UserID,
		WSConn:      conn,
		TokenHash:   hash,
		Description: reg.Description,
		Hardware:    reg.Hardware,
		Models:      reg.Models,
		MaxConcurrent: reg.MaxConcurrent,
	}
	if sess.MaxConcurrent <= 0 {
		sess.MaxConcurrent = 1
	}

	s.registry.Register(sess)

	if err := sess.SendWS(proto.RegisteredMsg{
		Type:       proto.TypeRegistered,
		MachineID:  machine.ID,
		ProviderID: sessionID,
	}); err != nil {
		log.Printf("ws: send registered error: %v", err)
		s.registry.Unregister(machine.ID)
		_ = conn.Close()
		return
	}

	_ = conn.SetReadDeadline(time.Time{})
	readDone := make(chan struct{})
	go func() {
		s.readLoop(sess)
		close(readDone)
	}()

	<-readDone

	userID, sessionReqs, sessionTokens := s.registry.Unregister(machine.ID)
	log.Printf("ws: provider disconnected machine_id=%s requests=%d tokens=%d",
		machine.ID, sessionReqs, sessionTokens)

	if userID > 0 && (sessionReqs > 0 || sessionTokens > 0) {
		uptimeSec := int64(time.Since(sess.ConnectedAt).Seconds())
		if err := s.store.UpdateOwnerStats(userID, int64(sessionReqs), int64(sessionTokens), uptimeSec); err != nil {
			log.Printf("ws: update owner stats error: %v", err)
		}
	}
}

func (s *Server) readLoop(sess *MachineSession) {
	defer func() { _ = sess.WSConn.Close() }()

	for {
		_, raw, err := sess.WSConn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("ws: read error machine_id=%s: %v", sess.MachineID, err)
			}
			return
		}

		var env proto.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			log.Printf("ws: invalid message from %s: %v", sess.MachineID, err)
			continue
		}

		switch env.Type {
		case proto.TypeHeartbeat:
			if sess.TokenHash != "" {
				if k, _ := s.store.FindKeyByHash(sess.TokenHash); k == nil {
					log.Printf("ws: token revoked, disconnecting machine_id=%s", sess.MachineID)
					return
				}
			}
			s.registry.UpdateHeartbeat(sess.MachineID)
			_ = sess.SendWS(proto.HeartbeatAckMsg{Type: proto.TypeHeartbeatAck})

		case proto.TypeChunk:
			var msg proto.ChunkMsg
			if err := json.Unmarshal(raw, &msg); err != nil {
				log.Printf("ws: invalid chunk: %v", err)
				continue
			}
			s.relayChunk(sess.MachineID, msg)

		case proto.TypeResponse:
			var msg proto.ResponseMsg
			if err := json.Unmarshal(raw, &msg); err != nil {
				log.Printf("ws: invalid response: %v", err)
				continue
			}
			log.Printf("ws: received response machine_id=%s request_id=%s", sess.MachineID, msg.RequestID)
			s.relayResponse(sess.MachineID, msg)

		case proto.TypeError:
			var msg proto.ErrorMsg
			if err := json.Unmarshal(raw, &msg); err != nil {
				log.Printf("ws: invalid error: %v", err)
				continue
			}
			s.relayError(sess.MachineID, msg)
			if msg.Code == proto.ErrBackendUnavailable {
				s.registry.SetBackendOK(sess.MachineID, false)
			}

		default:
			log.Printf("ws: unknown message type %q from %s", env.Type, sess.MachineID)
		}
	}
}

func (s *Server) relayChunk(machineID string, msg proto.ChunkMsg) {
	sess := s.registry.GetSession(machineID)
	if sess == nil {
		return
	}
	sess.mu.Lock()
	ch, ok := sess.chunkCh[msg.RequestID]
	sess.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- ChunkRelay{Content: msg.Content, Done: msg.Done}:
	default:
	}
	if msg.Done {
		sess.UnregisterChunkChannel(msg.RequestID)
	}
	s.registry.AddTokens(machineID, 1)
	atomic.AddInt64(&s.tokensToday, 1)
	_ = s.store.UpdateOwnerStats(sess.UserID, 0, 1, 0)
}

func (s *Server) relayResponse(machineID string, msg proto.ResponseMsg) {
	sess := s.registry.GetSession(machineID)
	if sess == nil {
		log.Printf("relayResponse: session not found machine_id=%s request_id=%s", machineID, msg.RequestID)
		return
	}

	var usage struct {
		CompletionTokens int `json:"completion_tokens"`
	}
	if len(msg.Usage) > 0 {
		_ = json.Unmarshal(msg.Usage, &usage)
	}

	sess.mu.Lock()
	ch, ok := sess.chunkCh[msg.RequestID]
	sess.mu.Unlock()
	if !ok {
		log.Printf("relayResponse: no chunk channel for request_id=%s", msg.RequestID)
		return
	}
	select {
	case ch <- ChunkRelay{Content: msg.Content, Tokens: usage.CompletionTokens}:
	default:
		log.Printf("relayResponse: channel full for request_id=%s", msg.RequestID)
	}
	sess.UnregisterChunkChannel(msg.RequestID)

	if usage.CompletionTokens > 0 {
		s.registry.AddTokens(machineID, usage.CompletionTokens)
		atomic.AddInt64(&s.tokensToday, int64(usage.CompletionTokens))
		_ = s.store.UpdateOwnerStats(sess.UserID, 1, int64(usage.CompletionTokens), 0)
	}
}

func (s *Server) relayError(machineID string, msg proto.ErrorMsg) {
	sess := s.registry.GetSession(machineID)
	if sess == nil {
		return
	}
	sess.mu.Lock()
	ch, ok := sess.chunkCh[msg.RequestID]
	sess.mu.Unlock()
	if ok {
		select {
		case ch <- ChunkRelay{Err: msg.Code + ": " + msg.Message}:
		default:
		}
		sess.UnregisterChunkChannel(msg.RequestID)
	}
}

func generateSessionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
