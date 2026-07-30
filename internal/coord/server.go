package coord

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/r00takaspin/gpumesh/internal/proto"
	"github.com/r00takaspin/gpumesh/web"
)

// Server is the coordinator HTTP server.
type Server struct {
	addr     string
	store    *Store
	registry *Registry
	limiter  *RateLimiter
	pinLimiter *pinLimiter
	baseURL  string
	srv      *http.Server

	inviteTTLDays   int
	inviteMaxUses   int
	pinAttemptLimit int

	startTime     time.Time
	requestsToday int64
	tokensToday   int64
}

// Config holds server configuration.
type Config struct {
	Addr            string
	DBPath          string
	BaseURL         string
	RateLimit       int // requests per hour per key
	InviteTTLDays   int
	InviteMaxUses   int
	PinAttemptLimit int
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
	if cfg.InviteTTLDays <= 0 {
		cfg.InviteTTLDays = 7
	}
	if cfg.InviteMaxUses <= 0 {
		cfg.InviteMaxUses = 1
	}
	if cfg.PinAttemptLimit <= 0 {
		cfg.PinAttemptLimit = 10
	}

	s := &Server{
		addr:            cfg.Addr,
		store:           store,
		registry:        NewRegistry(),
		limiter:         RateLimitHourly(cfg.RateLimit),
		pinLimiter:      newPinLimiter(cfg.PinAttemptLimit),
		baseURL:         cfg.BaseURL,
		startTime:       time.Now(),
		inviteTTLDays:   cfg.InviteTTLDays,
		inviteMaxUses:   cfg.InviteMaxUses,
		pinAttemptLimit: cfg.PinAttemptLimit,
	}

	mux := http.NewServeMux()

	// Public pages.
	mux.HandleFunc("GET /", s.corsMiddleware(s.handleIndex))
	mux.HandleFunc("GET /models", s.redirectUse) // public catalog removed in v2
	mux.HandleFunc("GET /about", s.corsMiddleware(s.handleAbout))
	mux.HandleFunc("GET /join", s.corsMiddleware(s.handleJoinPage))

	mux.HandleFunc("GET /use", s.corsMiddleware(s.handleUse))
	mux.HandleFunc("GET /share", s.corsMiddleware(s.handleShare))
	mux.HandleFunc("GET /dashboard", s.redirectUse)

	// OAuth.
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("GET /auth/github", s.handleLoginStart)
	mux.HandleFunc("GET /auth/github/callback", s.handleGitHubCallback)
	mux.HandleFunc("GET /logout", s.handleLogout)

	// OpenAI-compatible API (v2 per-machine).
	mux.HandleFunc("GET /v1/models", s.corsMiddleware(s.requireAPIKey(s.handleAPIModels)))
	mux.HandleFunc("GET /v1/machines/{machine_id}/models", s.corsMiddleware(s.requireAPIKey(s.handleMachineModels)))
	mux.HandleFunc("POST /v1/machines/{machine_id}/chat/completions", s.corsMiddleware(s.requireAPIKey(s.handleMachineChatCompletions)))
	mux.HandleFunc("POST /v1/chat/completions", s.corsMiddleware(s.requireAPIKey(s.handleLegacyChatCompletions)))
	mux.HandleFunc("OPTIONS /v1/", s.handleCORS)
	mux.HandleFunc("GET /v1/", s.corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	}))
	mux.HandleFunc("POST /v1/", s.corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	}))
	mux.HandleFunc("GET /api/", s.corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	}))
	mux.HandleFunc("POST /api/", s.corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	}))
	mux.HandleFunc("GET /ws/provider", s.handleWSProvider)

	// API key management.
	mux.HandleFunc("POST /api/keys", s.requireAuth(s.handleCreateKey))
	mux.HandleFunc("GET /api/keys", s.requireAuth(s.handleListKeys))
	mux.HandleFunc("DELETE /api/keys/{id}", s.requireAuth(s.handleRevokeKey))
	mux.HandleFunc("POST /api/keys/{id}/regenerate", s.requireAuth(s.handleRegenerateKey))

	mux.HandleFunc("POST /api/report", s.requireAPIKey(s.handleReport))

	// Stats.
	mux.HandleFunc("GET /api/consumer/stats", s.requireAuth(s.handleConsumerStats))
	mux.HandleFunc("GET /api/owner/stats", s.requireAuth(s.handleOwnerStatsAPI))
	mux.HandleFunc("GET /api/owner/status", s.requireAuth(s.handleOwnerStatus))

	// Invites / bindings.
	mux.HandleFunc("POST /api/invites", s.requireAuth(s.handleCreateInvite))
	mux.HandleFunc("GET /api/invites", s.requireAuth(s.handleListInvites))
	mux.HandleFunc("DELETE /api/invites/{id}", s.requireAuth(s.handleRevokeInvite))
	mux.HandleFunc("POST /api/join", s.requireAuth(s.handleJoin))
	mux.HandleFunc("GET /api/bindings", s.requireAuth(s.handleListBindings))
	mux.HandleFunc("DELETE /api/bindings/{machine_id}", s.requireAuth(s.handleRevokeBinding))
	mux.HandleFunc("DELETE /api/machines/{machine_id}/members/{user_id}", s.requireAuth(s.handleRevokeMember))

	// HTMX fragments (existing v1 templates; keep working).
	mux.HandleFunc("GET /use/keys", s.requireAuth(s.handleUseKeys))
	mux.HandleFunc("POST /use/keys", s.requireAuth(s.handleUseCreateKey))
	mux.HandleFunc("GET /use/donor", s.requireAuth(s.handleUseDonor))
	mux.HandleFunc("GET /share/setup", s.requireAuth(s.handleShareSetup))
	mux.HandleFunc("GET /share/models", s.requireAuth(s.handleShareModels))
	mux.HandleFunc("GET /share/donor-stats", s.requireAuth(s.handleShareDonorStats))
	mux.HandleFunc("GET /share/stats", s.requireAuth(s.handleShareDonorStats))
	mux.HandleFunc("POST /share/tokens", s.requireAuth(s.handleShareCreateToken))

	mux.HandleFunc("GET /health", s.handleHealth)

	mux.HandleFunc("GET /test/session", testModeOnly(s.handleTestSession))
	mux.HandleFunc("GET /test/session-token", testModeOnly(s.handleTestSessionToken))
	mux.HandleFunc("GET /test/error", testModeOnly(s.handleTestError))
	mux.HandleFunc("POST /test/reset-rate-limit", testModeOnly(s.handleTestResetRateLimit))
	mux.HandleFunc("POST /test/set-machine-load", testModeOnly(s.handleTestSetMachineLoad))

	mux.HandleFunc("GET /install-provider.sh", s.handleInstallScript)

	staticFS, _ := fs.Sub(web.EmbeddedFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	s.srv = &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}

	return s, nil
}

// ListenAndServe starts the server and blocks until shutdown.
func (s *Server) ListenAndServe() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.registry.StartHeartbeatMonitor(ctx, proto.HeartbeatTimeout, proto.HeartbeatMonitorTick)
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, sess := range s.registry.AllSessions() {
					_ = s.store.UpdateOwnerStats(sess.UserID, 0, 0, 60)
				}
			}
		}
	}()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = s.srv.Shutdown(shutdownCtx)
	}()

	log.Printf("coordinator listening on %s", s.addr)
	return s.srv.ListenAndServe()
}

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

func (s *Server) handleCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "index.html", s.pageDataWithStats(r))
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "about.html", s.pageDataWithStats(r))
}

func (s *Server) redirectUse(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/use", http.StatusMovedPermanently)
}
