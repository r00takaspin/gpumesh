package coord

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

var oauthConfig *oauth2.Config

func initOAuthConfig(baseURL string) {
	oauthConfig = &oauth2.Config{
		ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		RedirectURL:  baseURL + "/auth/github/callback",
		Scopes:       []string{"user:email"},
		Endpoint:     github.Endpoint,
	}
}

// handleLoginStart redirects to GitHub OAuth.
func (s *Server) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	if oauthConfig == nil {
		initOAuthConfig(s.baseURL)
	}
	if oauthConfig.ClientID == "" {
		http.Error(w, "GitHub OAuth not configured", http.StatusServiceUnavailable)
		return
	}
	state := r.URL.Query().Get("redirect")
	if state == "" {
		state = "/dashboard"
	}
	http.Redirect(w, r, oauthConfig.AuthCodeURL(state), http.StatusFound)
}

// handleGitHubCallback handles the OAuth callback from GitHub.
func (s *Server) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	if oauthConfig == nil {
		initOAuthConfig(s.baseURL)
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	token, err := oauthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Printf("oauth exchange error: %v", err)
		http.Error(w, "auth failed", http.StatusInternalServerError)
		return
	}
	client := oauthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://api.github.com/user")
	if err != nil {
		log.Printf("github user fetch error: %v", err)
		http.Error(w, "failed to get user info", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var ghUser struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ghUser); err != nil {
		log.Printf("github user decode error: %v", err)
		http.Error(w, "failed to parse user info", http.StatusInternalServerError)
		return
	}
	userID, err := s.store.UpsertUser(ghUser.ID, ghUser.Login)
	if err != nil {
		log.Printf("upsert user error: %v", err)
		http.Error(w, "failed to save user", http.StatusInternalServerError)
		return
	}
	sessionToken, err := s.store.CreateSession(userID)
	if err != nil {
		log.Printf("create session error: %v", err)
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "gpumesh_session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		MaxAge:   86400,
		SameSite: http.SameSiteLaxMode,
	})
	redirect := r.URL.Query().Get("state")
	if redirect == "" {
		redirect = "/dashboard"
	}
	// First login with no keys → auto-create flow.
	if redirect == "/dashboard" || redirect == "/consumer" {
		n, _ := s.store.CountKeys(userID)
		if n == 0 {
			redirect = redirect + "?new=1"
		}
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

// handleLogout clears the session cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("gpumesh_session")
	if err == nil {
		s.store.DeleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "gpumesh_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

// requireAuth is middleware that checks the session cookie.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("gpumesh_session")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		userID, err := s.store.ValidateSession(cookie.Value)
		if err != nil || userID == 0 {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		// Store userID in context.
		ctx := context.WithValue(r.Context(), ctxKeyUserID, userID)
		next(w, r.WithContext(ctx))
	}
}

type contextKey string

const (
	ctxKeyUserID    contextKey = "userID"
	ctxKeyAPIKeyHash contextKey = "apiKeyHash"
)

func getUserID(r *http.Request) int64 {
	v := r.Context().Value(ctxKeyUserID)
	if v == nil {
		return 0
	}
	return v.(int64)
}

func (s *Server) getGithubLogin(userID int64) string {
	login, _ := s.store.GetUserByID(userID)
	return login
}

// handleDashboard renders the dashboard page.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	pd := s.pageDataWithStats(r)

	// Auto-create first key if requested and user has none.
	if r.URL.Query().Get("new") == "1" {
		n, _ := s.store.CountKeys(userID)
		if n == 0 {
			rawKey, _, err := s.store.CreateKey(userID, "consumer")
			if err == nil {
				pd.NewKey = rawKey
			}
		}
	}

	renderTemplate(w, "dashboard.html", pd)
}

// handleConsumer renders the consumer page.
func (s *Server) handleConsumer(w http.ResponseWriter, r *http.Request) {
	pd := s.pageDataWithStats(r)

	// Extract user ID from session (public page, no requireAuth wrapper).
	var userID int64
	if cookie, err := r.Cookie("gpumesh_session"); err == nil {
		uid, err := s.store.ValidateSession(cookie.Value)
		if err == nil && uid != 0 {
			userID = uid
		}
	}

	// Set rate limit.
	pd.RateLimit = s.limiter.Burst()
	// Set base URL for tool config snippets.
	pd.BaseURL = s.baseURL

	// Set active tab from query param, default to "overview".
	pd.Tab = r.URL.Query().Get("tab")
	if pd.Tab == "" {
		pd.Tab = "overview"
	}

	if userID != 0 {
		// Auto-create consumer key if none exists.
		if r.URL.Query().Get("new") == "1" {
			n, _ := s.store.CountKeys(userID)
			if n == 0 {
				rawKey, _, err := s.store.CreateKey(userID, "consumer")
				if err == nil {
					pd.NewKey = rawKey
				}
			}
		}

		// Populate keys for the API Keys tab.
		keys, _ := s.store.ListKeys(userID)
		pd.Keys = keys
	}

	renderTemplate(w, "consumer.html", pd)
}

// handleLoginPage renders the login page with a GitHub OAuth button.
func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "login.html", s.pageData(r))
}
