package coord

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gpumesh/gpumesh/internal/proto"
)

// Server is the coordinator HTTP server.
type Server struct {
	addr     string
	store    *Store
	registry *Registry
	limiter  *RateLimiter
	baseURL  string
	srv      *http.Server

	startTime time.Time
	// Daily counters (reset at midnight).
	requestsToday int64
	tokensToday   int64
}

// Config holds server configuration.
type Config struct {
	Addr     string
	DBPath   string
	BaseURL  string
	RateLimit int // requests per hour per key
}

// NewServer creates a new coordinator server.
func NewServer(cfg Config) (*Server, error) {
	store, err := NewStore(cfg.DBPath)
	if err != nil {
		return nil, err
	}

	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 100
	}

	s := &Server{
		addr:            cfg.Addr,
		store:           store,
		registry:        NewRegistry(),
		limiter:         RateLimitHourly(cfg.RateLimit),
		baseURL:         cfg.BaseURL,
		startTime:       time.Now(),
	}

	mux := http.NewServeMux()

	// Public pages.
	mux.HandleFunc("GET /", s.corsMiddleware(s.handleIndex))
	mux.HandleFunc("GET /models", s.corsMiddleware(s.handleModelsPage))
	mux.HandleFunc("GET /leaderboard", s.corsMiddleware(s.handleLeaderboardPage))
	mux.HandleFunc("GET /status", s.corsMiddleware(s.handleStatusPage))

	// Dashboard (auth required).
	mux.HandleFunc("GET /dashboard", s.requireAuth(s.handleDashboard))

	// OAuth.
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("GET /auth/github", s.handleLoginStart)
	mux.HandleFunc("GET /auth/github/callback", s.handleGitHubCallback)
	mux.HandleFunc("GET /logout", s.handleLogout)

	// OpenAI-compatible API.
	mux.HandleFunc("GET /v1/models", s.corsMiddleware(s.requireAPIKey(s.handleAPIModels)))
	mux.HandleFunc("POST /v1/chat/completions", s.corsMiddleware(s.requireAPIKey(s.handleAPIChatCompletions)))
	mux.HandleFunc("OPTIONS /v1/", s.handleCORS)
	mux.HandleFunc("GET /ws/provider", s.handleWSProvider)
	// API key management (auth required).
	mux.HandleFunc("POST /api/keys", s.requireAuth(s.handleCreateKey))
	mux.HandleFunc("GET /api/keys", s.requireAuth(s.handleListKeys))
	mux.HandleFunc("DELETE /api/keys/{id}", s.requireAuth(s.handleRevokeKey))
	mux.HandleFunc("POST /api/keys/{id}/regenerate", s.requireAuth(s.handleRegenerateKey))

	// Abuse reporting (API key auth).
	mux.HandleFunc("POST /api/report", s.requireAPIKey(s.handleReport))

	// Frontend data API.
	mux.HandleFunc("GET /api/status", s.corsMiddleware(s.handleAPIStatus))
	mux.HandleFunc("GET /api/consumer/stats", s.requireAuth(s.handleConsumerStats))
	mux.HandleFunc("GET /api/donor/stats", s.requireAuth(s.handleDonorStatsAPI))
	mux.HandleFunc("GET /api/donor/status", s.requireAuth(s.handleDonorStatus))
	mux.HandleFunc("GET /leaderboard/data", s.corsMiddleware(s.handleLeaderboardData))
	mux.HandleFunc("GET /models/data", s.corsMiddleware(s.handleModelsData))

	// HTMX dashboard fragments.
	mux.HandleFunc("GET /dashboard/consumer", s.requireAuth(s.handleDashboardConsumer))
	mux.HandleFunc("GET /dashboard/donor", s.requireAuth(s.handleDashboardDonor))

	// Health check.
	mux.HandleFunc("GET /health", s.handleHealth)

	// Static files.
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	s.srv = &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}

	return s, nil
}

// ListenAndServe starts the server and blocks until shutdown.
func (s *Server) ListenAndServe() error {
	// Start heartbeat monitor.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.registry.StartHeartbeatMonitor(ctx, proto.HeartbeatTimeout, proto.HeartbeatMonitorTick)

	// Graceful shutdown.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		s.srv.Shutdown(shutdownCtx)
	}()

	log.Printf("coordinator listening on %s", s.addr)
	return s.srv.ListenAndServe()
}

// corsMiddleware adds CORS headers for /v1/* endpoints.
func (s *Server) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

// handleCORS responds to preflight requests.
func (s *Server) handleCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.WriteHeader(http.StatusNoContent)
}

// --- Stub handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "index.html", s.pageData(r))
}

func (s *Server) handleModelsPage(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "models.html", s.pageData(r))
}

func (s *Server) handleLeaderboardPage(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "leaderboard.html", s.pageData(r))
}

func (s *Server) handleStatusPage(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "status.html", s.pageData(r))
}

