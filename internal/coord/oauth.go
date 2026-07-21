package coord

import (
	"context"
	"encoding/json"
	"fmt"
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

// handleLogin redirects to GitHub OAuth.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if oauthConfig == nil {
		initOAuthConfig(s.baseURL)
	}
	if oauthConfig.ClientID == "" {
		http.Error(w, "GitHub OAuth not configured", http.StatusServiceUnavailable)
		return
	}
	// Generate state for CSRF protection.
	state := r.URL.Query().Get("redirect")
	if state == "" {
		state = "/dashboard"
	}
	url := oauthConfig.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusFound)
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

	// Get GitHub user info.
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

	// Upsert user.
	userID, err := s.store.UpsertUser(ghUser.ID, ghUser.Login)
	if err != nil {
		log.Printf("upsert user error: %v", err)
		http.Error(w, "failed to save user", http.StatusInternalServerError)
		return
	}

	// Create session.
	sessionToken, err := s.store.CreateSession(userID)
	if err != nil {
		log.Printf("create session error: %v", err)
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	// Set session cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "gpumesh_session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		MaxAge:   86400, // 24 hours
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to dashboard or requested page.
	redirect := r.URL.Query().Get("state")
	if redirect == "" {
		redirect = "/dashboard"
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

const ctxKeyUserID contextKey = "userID"

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
	if userID == 0 {
		// Check cookie.
		cookie, err := r.Cookie("gpumesh_session")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		uid, err := s.store.ValidateSession(cookie.Value)
		if err != nil || uid == 0 {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		userID = uid
	}

	login := s.getGithubLogin(userID)
	_ = login
	// Template rendering will be implemented in Step 12.
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Dashboard</title></head><body><h1>Dashboard</h1><p>Welcome, %s</p><p><a href="/logout">Logout</a></p></body></html>`, login)
}
