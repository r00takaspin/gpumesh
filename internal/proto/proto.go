package proto

import (
	"encoding/json"
	"time"
)

// WebSocket message type constants (§3.4 SPEC).
const (
	TypeRegister     = "register"
	TypeRegistered   = "registered"
	TypeHeartbeat    = "heartbeat"
	TypeHeartbeatAck = "heartbeat_ack"
	TypeRequest      = "request"
	TypeChunk        = "chunk"
	TypeResponse     = "response"
	TypeError        = "error"
	TypeCancel       = "cancel"
)

// Error codes (§3.4 SPEC).
const (
	ErrBackendUnavailable = "backend_unavailable"
	ErrModelNotFound      = "model_not_found"
	ErrTimeout            = "timeout"
	ErrOverloaded         = "overloaded"
	ErrInternal           = "internal"
)

// Timeout constants (§3.7 SPEC).
const (
	TTFTTimeout          = 90 * time.Second  // covers cold model load (27B ~55s) + headroom
	InterTokenTimeout    = 30 * time.Second  // slow models / chain-of-thought
	TotalRequestTimeout  = 180 * time.Second // cold start + generation up to ~2000 tokens
	HeartbeatInterval    = 30 * time.Second
	HeartbeatTimeout     = 90 * time.Second
	HeartbeatMonitorTick = 15 * time.Second
)

// Max retries for non-streaming requests on donor failure (§3.5).
const MaxRetries = 3

// --- Donor → Coordinator messages ---

// RegisterMsg is sent by a donor on initial WebSocket connection.
type RegisterMsg struct {
	Type          string   `json:"type"`           // always "register"
	Models        []string `json:"models"`
	MaxConcurrent int      `json:"max_concurrent"`
	Description   string   `json:"description"`
	Hardware      string   `json:"hardware,omitempty"`
}

// HeartbeatMsg is sent periodically by the donor.
type HeartbeatMsg struct {
	Type string `json:"type"` // always "heartbeat"
}

// ChunkMsg is a streaming token chunk from a donor.
type ChunkMsg struct {
	Type      string `json:"type"`       // always "chunk"
	RequestID string `json:"request_id"`
	Content   string `json:"content"`
	Done      bool   `json:"done"`
}

// ResponseMsg is a complete non-streaming response from a donor.
type ResponseMsg struct {
	Type      string          `json:"type"`       // always "response"
	RequestID string          `json:"request_id"`
	Content   string          `json:"content"`
	Model     string          `json:"model"`
	Usage     json.RawMessage `json:"usage"`
}

// ErrorMsg is sent by a donor when it cannot fulfil a request.
type ErrorMsg struct {
	Type      string `json:"type"`       // always "error"
	RequestID string `json:"request_id"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

// --- Coordinator → Donor messages ---

// RegisteredMsg is sent after a successful donor registration.
type RegisteredMsg struct {
	Type       string `json:"type"`        // always "registered"
	ProviderID string `json:"provider_id"`
}

// RequestMsg is an inference request forwarded to a donor.
type RequestMsg struct {
	Type      string          `json:"type"`       // always "request"
	RequestID string          `json:"request_id"`
	Model     string          `json:"model"`
	Messages  json.RawMessage `json:"messages"`
	Stream    bool            `json:"stream"`
	Options   json.RawMessage `json:"options,omitempty"`
}

// CancelMsg is sent to a donor to cancel an in-progress request.
type CancelMsg struct {
	Type      string `json:"type"`       // always "cancel"
	RequestID string `json:"request_id"`
}

// HeartbeatAckMsg is sent in response to a heartbeat.
type HeartbeatAckMsg struct {
	Type string `json:"type"` // always "heartbeat_ack"
}

// --- Request/response types for consumer API ---

// ChatCompletionRequest is the OpenAI-compatible chat completion request body.
type ChatCompletionRequest struct {
	Model    string          `json:"model"`
	Messages json.RawMessage `json:"messages"`
	Stream   bool            `json:"stream,omitempty"`
	// Optional fields passed through to Ollama.
	Temperature float64         `json:"temperature,omitempty"`
	TopP        float64         `json:"top_p,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Extra       json.RawMessage `json:"-"` // passthrough unknown fields
}

// ModelEntry represents one model in /v1/models response (§3.3 SPEC).
type ModelEntry struct {
	ID           string  `json:"id"`
	Object       string  `json:"object"`       // always "model"
	OwnedBy      string  `json:"owned_by"`     // always "community"
	DonorsOnline int     `json:"donors_online"`
	Load         float64 `json:"load"`
}

// ModelListResponse is the response for GET /v1/models.
type ModelListResponse struct {
	Object string       `json:"object"` // always "list"
	Data   []ModelEntry `json:"data"`
}

// --- API key scopes ---

const (
	ScopeConsumer = "consumer"
	ScopeDonor    = "donor"
	ScopeBoth     = "both"
)

// --- Generic message envelope for raw WS reads ---

// Envelope is a generic JSON message with a type discriminator.
// Used for initial parsing of incoming WS messages.
type Envelope struct {
	Type string `json:"type"`
}
