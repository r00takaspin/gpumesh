package coord

import (
	"context"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// MachineSession represents a connected provider agent for one machine (§10.4 SPEC-v2).
type MachineSession struct {
	MachineID     string
	SessionID     string // ephemeral, for logs
	UserID        int64
	Models        []string
	MaxConcurrent int
	CurrentLoad   int
	Description   string
	Hardware      string
	ConnectedAt   time.Time
	TokenHash     string
	LastHeartbeat time.Time
	BackendOK     bool
	BackendFailedAt time.Time
	SessionRequests int
	SessionTokens   int
	WSConn          *websocket.Conn

	mu      sync.Mutex
	writeMu sync.Mutex
	requests map[string]context.CancelFunc
	chunkCh  map[string]chan<- ChunkRelay
}

// ChunkRelay carries a chunk and done flag from the provider read loop to the SSE writer.
type ChunkRelay struct {
	Content string
	Done    bool
	Err     string
	Tokens  int
}

// Registry is a thread-safe in-memory machine session registry (§10.4 SPEC-v2).
type Registry struct {
	mu       sync.RWMutex
	machines map[string]*MachineSession // machine_id → session
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		machines: make(map[string]*MachineSession),
	}
}

// Register adds a machine session to the registry.
func (r *Registry) Register(s *MachineSession) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If reconnecting, cancel old session first.
	if old, ok := r.machines[s.MachineID]; ok {
		old.mu.Lock()
		for _, cancel := range old.requests {
			cancel()
		}
		for _, ch := range old.chunkCh {
			close(ch)
		}
		old.mu.Unlock()
	}

	s.ConnectedAt = time.Now()
	s.LastHeartbeat = time.Now()
	s.BackendOK = true
	s.requests = make(map[string]context.CancelFunc)
	s.chunkCh = make(map[string]chan<- ChunkRelay)
	r.machines[s.MachineID] = s
}

// Unregister removes a machine session. Returns session counters for persistence.
func (r *Registry) Unregister(machineID string) (userID int64, sessionReqs int, sessionTokens int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.machines[machineID]
	if !ok {
		return 0, 0, 0
	}

	s.mu.Lock()
	for _, cancel := range s.requests {
		cancel()
	}
	s.requests = nil
	for _, ch := range s.chunkCh {
		close(ch)
	}
	s.chunkCh = nil
	s.mu.Unlock()

	userID = s.UserID
	sessionReqs = s.SessionRequests
	sessionTokens = s.SessionTokens
	delete(r.machines, machineID)
	return
}

// GetSession returns a machine session by machine_id.
func (r *Registry) GetSession(machineID string) *MachineSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.machines[machineID]
}

// UpdateHeartbeat records a heartbeat from a machine.
func (r *Registry) UpdateHeartbeat(machineID string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s := r.machines[machineID]; s != nil {
		s.LastHeartbeat = time.Now()
	}
}

// SetBackendOK marks the backend health status of a machine.
func (r *Registry) SetBackendOK(machineID string, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s := r.machines[machineID]; s != nil {
		s.BackendOK = ok
		if !ok {
			s.BackendFailedAt = time.Now()
		}
	}
}

// IncrementLoad attempts to increment load. Returns false if at max capacity.
func (r *Registry) IncrementLoad(machineID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := r.machines[machineID]
	if s == nil {
		return false
	}
	if s.CurrentLoad >= s.MaxConcurrent {
		return false
	}
	s.CurrentLoad++
	s.SessionRequests++
	return true
}

// DecrementLoad decrements a machine's load.
func (r *Registry) DecrementLoad(machineID string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s := r.machines[machineID]; s != nil {
		if s.CurrentLoad > 0 {
			s.CurrentLoad--
		}
	}
}

// AddTokens adds to the session token count.
func (r *Registry) AddTokens(machineID string, tokens int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s := r.machines[machineID]; s != nil {
		s.SessionTokens += tokens
	}
}

// HasModel returns true if the online session advertises the model.
func (r *Registry) HasModel(machineID, model string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := r.machines[machineID]
	if s == nil || !s.BackendOK {
		return false
	}
	for _, m := range s.Models {
		if m == model {
			return true
		}
	}
	return false
}

// RegisterChunkChannel stores a relay channel for a pending request.
func (s *MachineSession) RegisterChunkChannel(requestID string, ch chan<- ChunkRelay) {
	s.mu.Lock()
	s.chunkCh[requestID] = ch
	s.mu.Unlock()
}

// UnregisterChunkChannel removes the relay channel.
func (s *MachineSession) UnregisterChunkChannel(requestID string) {
	s.mu.Lock()
	delete(s.chunkCh, requestID)
	s.mu.Unlock()
}

// CancelAllRequests cancels in-flight requests (used on binding revoke).
func (s *MachineSession) CancelAllRequests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, cancel := range s.requests {
		cancel()
		delete(s.requests, id)
	}
	for id, ch := range s.chunkCh {
		close(ch)
		delete(s.chunkCh, id)
	}
}

// SendWS sends a JSON message over the provider WebSocket connection.
func (s *MachineSession) SendWS(v interface{}) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.WSConn.WriteJSON(v)
}

// LoadFraction returns current_load / max_concurrent.
func (s *MachineSession) LoadFraction() float64 {
	if s.MaxConcurrent <= 0 {
		return 0
	}
	return float64(s.CurrentLoad) / float64(s.MaxConcurrent)
}

// AllSessions returns all connected machine sessions.
func (r *Registry) AllSessions() []*MachineSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*MachineSession, 0, len(r.machines))
	for _, s := range r.machines {
		out = append(out, s)
	}
	return out
}

// MachinesForUser returns all online sessions owned by a user.
func (r *Registry) MachinesForUser(userID int64) []*MachineSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*MachineSession
	for _, s := range r.machines {
		if s.UserID == userID {
			result = append(result, s)
		}
	}
	return result
}

// OnlineCount returns number of connected machines.
func (r *Registry) OnlineCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.machines)
}

// RegistrySnapshot is a summary for landing/UI stats.
type RegistrySnapshot struct {
	DonorsOnline int // machines online (kept field name for UI templates)
	ModelsOnline int
	Models       map[string]ModelSnapshot
}

// ModelSnapshot aggregates one model across online machines.
type ModelSnapshot struct {
	DonorsOnline int
	Load         float64
}

// Snapshot returns registry state for UI pages.
func (r *Registry) Snapshot() RegistrySnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	modelIndex := make(map[string][]*MachineSession)
	for _, s := range r.machines {
		if !s.BackendOK {
			continue
		}
		for _, m := range s.Models {
			modelIndex[m] = append(modelIndex[m], s)
		}
	}

	snap := RegistrySnapshot{
		DonorsOnline: len(r.machines),
		ModelsOnline: len(modelIndex),
		Models:       make(map[string]ModelSnapshot, len(modelIndex)),
	}
	for model, sessions := range modelIndex {
		var sumLoad float64
		for _, s := range sessions {
			sumLoad += s.LoadFraction()
		}
		avg := 0.0
		if len(sessions) > 0 {
			avg = (sumLoad / float64(len(sessions))) * 100
		}
		snap.Models[model] = ModelSnapshot{
			DonorsOnline: len(sessions),
			Load:         avg,
		}
	}
	return snap
}

// BadgeForTokens returns a badge tier from lifetime token count.
func BadgeForTokens(tokens int64) string {
	switch {
	case tokens >= 1_000_000:
		return "platinum"
	case tokens >= 100_000:
		return "gold"
	case tokens >= 10_000:
		return "silver"
	case tokens >= 1_000:
		return "bronze"
	default:
		return "beginner"
	}
}

// StartHeartbeatMonitor removes sessions that haven't sent a heartbeat within the timeout.
func (r *Registry) StartHeartbeatMonitor(ctx context.Context, timeout time.Duration, tick time.Duration) {
	go func() {
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.evictStale(timeout)
			}
		}
	}()
}

const backendUnhealthyEviction = 5 * time.Minute

func (r *Registry) evictStale(timeout time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for mid, s := range r.machines {
		evict := false
		if now.Sub(s.LastHeartbeat) > timeout {
			evict = true
		}
		if !s.BackendOK && !s.BackendFailedAt.IsZero() && now.Sub(s.BackendFailedAt) > backendUnhealthyEviction {
			evict = true
		}
		if !evict {
			continue
		}

		s.mu.Lock()
		for _, cancel := range s.requests {
			cancel()
		}
		for _, ch := range s.chunkCh {
			close(ch)
		}
		s.mu.Unlock()
		delete(r.machines, mid)
	}
}
