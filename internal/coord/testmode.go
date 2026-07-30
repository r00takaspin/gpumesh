package coord

import (
	"hash/fnv"
	"net/http"
	"os"
	"strconv"

	"github.com/r00takaspin/gpumesh/internal/proto"
)

// testModeEnabled returns true if TEST_MODE=true.
func testModeEnabled() bool {
	return os.Getenv("TEST_MODE") == "true"
}

// testModeOnly is middleware that returns 404 if TEST_MODE is not enabled.
func testModeOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !testModeEnabled() {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

// testGithubID converts a test user login to a deterministic, non-zero github_id.
func testGithubID(login string) int64 {
	h := fnv.New64a()
	h.Write([]byte(login))
	// Use lower 53 bits to stay safely within JS integer range and avoid negative.
	return int64(h.Sum64() & 0x1FFFFFFFFFFFFF)
}

// handleTestSession creates a session cookie for the given user in test mode.
// GET /test/session?user=<login>
func (s *Server) handleTestSession(w http.ResponseWriter, r *http.Request) {
	login := r.URL.Query().Get("user")
	if login == "" {
		login = "testuser"
	}

	// Upsert the test user with a unique github_id derived from the login.
	userID, err := s.store.UpsertUser(testGithubID(login), login)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create test user")
		return
	}

	// Create a session.
	token, err := s.store.CreateSession(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "gpumesh_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})

	// Redirect to the requested page or /.
	redirect := r.URL.Query().Get("redirect")
	if redirect == "" {
		redirect = "/"
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// handleTestSessionToken returns the session token as JSON (no redirect),
// suitable for API tests that need to set the cookie manually.
// GET /test/session-token?user=<login>
func (s *Server) handleTestSessionToken(w http.ResponseWriter, r *http.Request) {
	login := r.URL.Query().Get("user")
	if login == "" {
		login = "testuser"
	}

	userID, err := s.store.UpsertUser(testGithubID(login), login)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create test user")
		return
	}

	token, err := s.store.CreateSession(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token":   token,
		"cookie":  "gpumesh_session=" + token,
		"user_id": userID,
		"login":   login,
	})
}

// handleTestAuthGitHub mimics GitHub OAuth callback in test mode.
// GET /auth/github?redirect=<path>&test_user=<login>
func (s *Server) handleTestAuthGitHub(w http.ResponseWriter, r *http.Request) {
	login := r.URL.Query().Get("test_user")
	if login == "" {
		login = "testuser"
	}
	redirect := r.URL.Query().Get("redirect")
	if redirect == "" {
		redirect = "/use"
	}

	// Upsert the test user (githubID=0 for test users).
	userID, err := s.store.UpsertUser(0, login)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create test user")
		return
	}

	// Create a session.
	token, err := s.store.CreateSession(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "gpumesh_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})

	// Auto-create an API key if on first login with redirect=/use or no keys exist.
	count, _ := s.store.CountKeys(userID)
	if count == 0 && (redirect == "/use" || redirect == "/") {
		// Create a consumer key silently.
		_, _, _ = s.store.CreateKey(userID, proto.ScopeConsumer)
		redirect = redirect + "?new=1"
	}

	http.Redirect(w, r, redirect, http.StatusSeeOther)
}
// GET /test/error?code=<N>
func (s *Server) handleTestError(w http.ResponseWriter, r *http.Request) {
	codeStr := r.URL.Query().Get("code")
	code, err := strconv.Atoi(codeStr)
	if err != nil || code < 400 {
		code = 500
	}

	templateName := "error-" + strconv.Itoa(code) + ".html"
	// Verify template exists; fall back to error-500.
	tmpls := getTemplates()
	if _, ok := tmpls[templateName]; !ok {
		templateName = "error-500.html"
	}

	pd := s.pageData(r)
	pd.Title = "Error " + strconv.Itoa(code)
	pd.ErrorCode = &code
	w.WriteHeader(code)
	renderTemplate(w, templateName, pd)
}

// handleTestResetRateLimit resets the token bucket for a key.
// POST /test/reset-rate-limit?key=<raw-key>
// Accepts the raw API key and hashes it internally.
func (s *Server) handleTestResetRateLimit(w http.ResponseWriter, r *http.Request) {
	rawKey := r.URL.Query().Get("key")
	if rawKey == "" {
		writeError(w, http.StatusBadRequest, "missing key parameter")
		return
	}

	s.limiter.Reset(hashKey(rawKey))
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

// handleTestSetMachineLoad sets the current load of a machine session.
// POST /test/set-machine-load?machine=<id>&load=<N>
func (s *Server) handleTestSetMachineLoad(w http.ResponseWriter, r *http.Request) {
	machineID := r.URL.Query().Get("machine")
	loadStr := r.URL.Query().Get("load")
	if machineID == "" || loadStr == "" {
		writeError(w, http.StatusBadRequest, "machine and load params required")
		return
	}
	load, err := strconv.Atoi(loadStr)
	if err != nil || load < 0 {
		writeError(w, http.StatusBadRequest, "invalid load")
		return
	}
	sess := s.registry.GetSession(machineID)
	if sess == nil {
		writeError(w, http.StatusNotFound, "machine not found")
		return
	}
	sess.CurrentLoad = load
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "load": load})
}
