package coord

import (
	"context"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Donor represents a connected provider agent (§5.3 SPEC).
type Donor struct {
	ProviderID    string
	UserID        int64
	Models        []string
	MaxConcurrent int
	CurrentLoad   int
	Description   string
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	BackendOK       bool
	BackendFailedAt time.Time // when BackendOK was set to false
	// Session counters reset on disconnect.
	SessionRequests int
	SessionTokens   int
	WSConn          *websocket.Conn

	mu        sync.Mutex
	writeMu   sync.Mutex // guards WS writes
	requests  map[string]context.CancelFunc
	chunkCh   map[string]chan<- ChunkRelay // request_id → consumer relay channel
}

// ChunkRelay carries a chunk and done flag from the donor read loop to the SSE writer.
type ChunkRelay struct {
	Content string
	Done    bool
	Err     string // non-empty on error
	Tokens  int    // completion tokens for non-streaming responses
}

// Registry is a thread-safe in-memory donor registry (§5.3 SPEC).
type Registry struct {
	mu         sync.RWMutex
	donors     map[string]*Donor          // provider_id → Donor
	modelIndex map[string]map[string]bool // model_name → set of provider_ids
}

// NewRegistry creates an empty registry and starts the heartbeat monitor.
func NewRegistry() *Registry {
	r := &Registry{
		donors:     make(map[string]*Donor),
		modelIndex: make(map[string]map[string]bool),
	}
	return r
}

// Register adds a donor to the registry and updates indices.
func (r *Registry) Register(d *Donor) {
	r.mu.Lock()
	defer r.mu.Unlock()

	d.ConnectedAt = time.Now()
	d.LastHeartbeat = time.Now()
	d.BackendOK = true
	d.requests = make(map[string]context.CancelFunc)
	d.chunkCh = make(map[string]chan<- ChunkRelay)
	r.donors[d.ProviderID] = d

	for _, m := range d.Models {
		if r.modelIndex[m] == nil {
			r.modelIndex[m] = make(map[string]bool)
		}
		r.modelIndex[m][d.ProviderID] = true
	}
}

// Unregister removes a donor and cleans up indices.
// Returns session counters for persistence.
func (r *Registry) Unregister(providerID string) (userID int64, sessionReqs int, sessionTokens int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	d, ok := r.donors[providerID]
	if !ok {
		return 0, 0, 0
	}

	// Cancel all pending requests for this donor.
	d.mu.Lock()
	for _, cancel := range d.requests {
		cancel()
	}
	d.requests = nil
	// Close relay channels.
	for _, ch := range d.chunkCh {
		close(ch)
	}
	d.chunkCh = nil
	d.mu.Unlock()

	// Clean model index.
	for _, m := range d.Models {
		delete(r.modelIndex[m], providerID)
		if len(r.modelIndex[m]) == 0 {
			delete(r.modelIndex, m)
		}
	}

	userID = d.UserID
	sessionReqs = d.SessionRequests
	sessionTokens = d.SessionTokens
	delete(r.donors, providerID)
	return
}

// FindDonorsForModel returns all donors with the given model and backend_ok=true.
func (r *Registry) FindDonorsForModel(model string) []*Donor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providerIDs := r.modelIndex[model]
	if len(providerIDs) == 0 {
		return nil
	}

	var donors []*Donor
	for pid := range providerIDs {
		d := r.donors[pid]
		if d != nil && d.BackendOK {
			donors = append(donors, d)
		}
	}
	return donors
}

// UpdateHeartbeat records a heartbeat from a donor.
func (r *Registry) UpdateHeartbeat(providerID string) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if d := r.donors[providerID]; d != nil {
		d.LastHeartbeat = time.Now()
	}
}

// SetBackendOK marks the backend health status of a donor.
func (r *Registry) SetBackendOK(providerID string, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if d := r.donors[providerID]; d != nil {
		d.BackendOK = ok
		if !ok {
			d.BackendFailedAt = time.Now()
		}
	}
}

// IncrementLoad attempts to increment a donor's load. Returns false if at max capacity.
func (r *Registry) IncrementLoad(providerID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	d := r.donors[providerID]
	if d == nil {
		return false
	}
	// Race-y but acceptable for MVP; the donor also checks.
	if d.CurrentLoad >= d.MaxConcurrent {
		return false
	}
	d.CurrentLoad++
	d.SessionRequests++
	return true
}

// DecrementLoad decrements a donor's load.
func (r *Registry) DecrementLoad(providerID string) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if d := r.donors[providerID]; d != nil {
		if d.CurrentLoad > 0 {
			d.CurrentLoad--
		}
	}
}

// AddTokens adds to the donor's session token count.
func (r *Registry) AddTokens(providerID string, tokens int) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if d := r.donors[providerID]; d != nil {
		d.SessionTokens += tokens
	}
}

// GetDonor returns a donor by provider ID.
func (r *Registry) GetDonor(providerID string) *Donor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.donors[providerID]
}

// RegisterChunkChannel stores a relay channel for a pending request.
// The donor's read loop writes chunks to this channel.
func (d *Donor) RegisterChunkChannel(requestID string, ch chan<- ChunkRelay) {
	d.mu.Lock()
	d.chunkCh[requestID] = ch
	d.mu.Unlock()
}

// UnregisterChunkChannel removes the relay channel for a completed/cancelled request.
func (d *Donor) UnregisterChunkChannel(requestID string) {
	d.mu.Lock()
	delete(d.chunkCh, requestID)
	d.mu.Unlock()
}

// RegisterCancel stores a cancel function for a pending request.
func (d *Donor) RegisterCancel(requestID string, cancel context.CancelFunc) {
	d.mu.Lock()
	d.requests[requestID] = cancel
	d.mu.Unlock()
}

// UnregisterCancel removes the cancel function.
func (d *Donor) UnregisterCancel(requestID string) {
	d.mu.Lock()
	delete(d.requests, requestID)
	d.mu.Unlock()
}

// SendWS sends a JSON message over the donor WebSocket connection.
func (d *Donor) SendWS(v interface{}) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	return d.WSConn.WriteJSON(v)
}

// Snapshot returns a summary of the registry state.
type RegistrySnapshot struct {
	DonorsOnline int
	ModelsOnline int
	Models       map[string]ModelSnapshot
}

type ModelSnapshot struct {
	DonorsOnline int
	Load         float64
}

// Snapshot returns the current registry state for /api/status.
func (r *Registry) Snapshot() RegistrySnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snap := RegistrySnapshot{
		DonorsOnline: len(r.donors),
		ModelsOnline: len(r.modelIndex),
		Models:       make(map[string]ModelSnapshot, len(r.modelIndex)),
	}

	for model, pids := range r.modelIndex {
		var sumLoad float64
		var count int
		for pid := range pids {
			if d := r.donors[pid]; d != nil && d.BackendOK {
				if d.MaxConcurrent > 0 {
					sumLoad += float64(d.CurrentLoad) / float64(d.MaxConcurrent)
				}
				count++
			}
		}
		avgLoad := 0.0
		if count > 0 {
			avgLoad = (sumLoad / float64(count)) * 100
		}
		snap.Models[model] = ModelSnapshot{
			DonorsOnline: count,
			Load:         avgLoad,
		}
	}

	return snap
}


// AllDonors returns all connected donors for stats persistence.
func (r *Registry) AllDonors() []*Donor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	donors := make([]*Donor, 0, len(r.donors))
	for _, d := range r.donors {
		donors = append(donors, d)
	}
	return donors
}
// DonorsForUser returns all donor connections for a given user.
func (r *Registry) DonorsForUser(userID int64) []*Donor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*Donor
	for _, d := range r.donors {
		if d.UserID == userID {
			result = append(result, d)
		}
	}
	return result
}

// StartHeartbeatMonitor removes donors that haven't sent a heartbeat within the timeout.
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
	for pid, d := range r.donors {
		evict := false
		if now.Sub(d.LastHeartbeat) > timeout {
			evict = true
		}
		// SPEC §5.4: evict donors with backend_ok=false for >5 minutes.
		if !d.BackendOK && !d.BackendFailedAt.IsZero() && now.Sub(d.BackendFailedAt) > backendUnhealthyEviction {
			evict = true
		}
		if !evict {
			continue
		}

		// Cancel all pending requests.
		d.mu.Lock()
		for _, cancel := range d.requests {
			cancel()
		}
		for _, ch := range d.chunkCh {
			close(ch)
		}
		d.mu.Unlock()

		// Clean model index.
		for _, m := range d.Models {
			delete(r.modelIndex[m], pid)
			if len(r.modelIndex[m]) == 0 {
				delete(r.modelIndex, m)
			}
		}
		delete(r.donors, pid)
	}
}
// LeaderboardEntry is a row in the donor leaderboard.
type LeaderboardEntry struct {
	Rank        int    `json:"rank"`
	GithubLogin string `json:"github_login"`
	AvatarURL   string `json:"avatar_url"`
	Tokens      int64  `json:"tokens"`
	Requests    int64  `json:"requests"`
	Badge       string `json:"badge"`
}

// Badge thresholds (§6.2.2 SPEC).
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
		return ""
	}
}
