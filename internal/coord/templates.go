package coord

import (
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/r00takaspin/gpumesh/web"
)

// PageData is passed to every full-page template render.
type PageData struct {
	LoggedIn         bool
	Login            string
	HasOAuth         bool
	HasKeys          bool
	NewKey           string
	Title            string
	ActiveNav        string
	Pin              string
	Redirect         string
	HasProviderScope bool
	HasDonorScope    bool // legacy alias for HasProviderScope
	Tab              string
	Keys             []APIKey
	RateLimit        int
	BaseURL          string
	ErrorCode        *int
}

var (
	templates     map[string]*template.Template
	templatesOnce sync.Once
)

func getTemplates() map[string]*template.Template {
	templatesOnce.Do(func() {
		templates = make(map[string]*template.Template)
		entries, err := web.EmbeddedFS.ReadDir("templates")
		if err != nil {
			log.Printf("templates: readdir: %v", err)
			return
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".html" {
				continue
			}
			if e.Name() == "chrome.html" {
				continue
			}
			// Parse page first so Execute runs the page, then add chrome defines.
			tmpl, err := template.New(e.Name()).ParseFS(web.EmbeddedFS, "templates/"+e.Name())
			if err != nil {
				log.Printf("templates: parse %s: %v", e.Name(), err)
				continue
			}
			tmpl, err = tmpl.ParseFS(web.EmbeddedFS, "templates/chrome.html")
			if err != nil {
				log.Printf("templates: parse chrome for %s: %v", e.Name(), err)
				continue
			}
			templates[e.Name()] = tmpl
		}
	})
	return templates
}

func renderTemplate(w http.ResponseWriter, name string, data any) {
	tmpl, ok := getTemplates()[name]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("templates: execute %s: %v", name, err)
	}
}

// pageData extracts PageData from the current request's auth state.
func (s *Server) pageData(r *http.Request) PageData {
	pd := PageData{BaseURL: strings.TrimRight(s.baseURL, "/")}
	cookie, err := r.Cookie("gpumesh_session")
	var uid int64
	if err == nil {
		uid, err = s.store.ValidateSession(cookie.Value)
		if err == nil && uid != 0 {
			pd.LoggedIn = true
			pd.Login, _ = s.store.GetUserByID(uid)
		}
	}
	if pd.LoggedIn {
		n, _ := s.store.CountKeys(uid)
		pd.HasKeys = n > 0
		keys, _ := s.store.ListKeys(uid)
		for _, k := range keys {
			if k.Scope == "provider" || k.Scope == "donor" || k.Scope == "both" {
				pd.HasProviderScope = true
				pd.HasDonorScope = true
				break
			}
		}
	}
	if oauthConfig == nil {
		initOAuthConfig(s.baseURL)
	}
	pd.HasOAuth = oauthConfig.ClientID != ""
	return pd
}

func formatRelative(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return formatDuration(d)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmtInt(h) + "h ago"
	default:
		days := int(d.Hours()) / 24
		if days == 1 {
			return "1d ago"
		}
		return fmtInt(days) + "d ago"
	}
}

func formatExpiresRel(t time.Time) string {
	d := time.Until(t)
	if d < 0 {
		return "expired"
	}
	days := int(d.Hours()) / 24
	if days <= 0 {
		return "soon"
	}
	if days == 1 {
		return "in 1d"
	}
	return "in " + fmtInt(days) + "d"
}

func fmtInt(n int) string {
	return strconv.Itoa(n)
}
