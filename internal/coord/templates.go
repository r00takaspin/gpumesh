package coord

import (
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/r00takaspin/gpumesh/web"
)

// PageData is passed to every template render.
type PageData struct {
	LoggedIn bool
	Login    string
	HasOAuth bool
	HasKeys  bool
	NewKey   string // full key, shown only once after auto-creation
	Title    string // optional page title override
	// Live data for dynamic pages.
	DonorsOnline  int
	ModelsOnline  int
	RequestsToday int
	StatsError    bool // true → hide stats block
	TokensToday   int64
	// Top models for landing page.
	TopModels []ModelSummary
	// Dashboard donor tab.
	HasDonorScope bool
	// Models page.
	Models []ModelData
	// Consumer page.
	Tab          string   // active tab: "overview", "keys", or "models"
	Keys         []APIKey // user's API keys for the consumer page
	RateLimit    int      // daily request limit
	BaseURL      string   // server base URL for tool config snippets
	DefaultModel string   // most popular model name (for "Try it now" block)
	// Error page support.
	ErrorCode *int // HTTP status code for error pages
}

// ModelSummary is a lightweight model entry for template rendering.
type ModelSummary struct {
	Name       string
	DonorCount int
	Vendor     string
}

// ModelData is a full model entry for the /models page.
type ModelData struct {
	Name         string
	DonorsOnline int
	Load         float64
	Tags         []string
	VRAM         string
	Vendor       string
}



// vendorForModel derives a vendor name from the model ID prefix.
func vendorForModel(name string) string {
	switch {
	case hasPrefix(name, "llama"), hasPrefix(name, "codellama"):
		return "Meta"
	case hasPrefix(name, "mistral"), hasPrefix(name, "mixtral"):
		return "Mistral AI"
	case hasPrefix(name, "qwen"):
		return "Alibaba"
	case hasPrefix(name, "phi"):
		return "Microsoft"
	case hasPrefix(name, "gemma"):
		return "Google"
	case hasPrefix(name, "deepseek"):
		return "DeepSeek"
	default:
		return "Community"
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// tagsForModel returns category tags for a model name.
func tagsForModel(name string) []string {
	switch {
	case hasPrefix(name, "llama"), hasPrefix(name, "codellama"), hasPrefix(name, "deepseek"):
		return []string{"chat", "code", "general"}
	case hasPrefix(name, "mistral"), hasPrefix(name, "mixtral"):
		return []string{"chat", "general"}
	case hasPrefix(name, "qwen"):
		return []string{"chat", "tiny", "edge"}
	case hasPrefix(name, "phi"):
		return []string{"chat", "code"}
	case hasPrefix(name, "nomic"):
		return []string{"embedding"}
	case hasPrefix(name, "gemma"):
		return []string{"chat", "general"}
	default:
		return []string{"chat"}
	}
}

// vramForModel estimates minimum VRAM from a model name.
func vramForModel(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "0.5b"), strings.Contains(lower, "embed"):
		return "2 GB"
	case strings.Contains(lower, "1b"), strings.Contains(lower, "1.5b"):
		return "2 GB"
	case strings.Contains(lower, "3b"):
		return "4 GB"
	case strings.Contains(lower, "7b"), strings.Contains(lower, "8b"):
		return "8 GB"
	case strings.Contains(lower, "13b"), strings.Contains(lower, "14b"):
		return "16 GB"
	case strings.Contains(lower, "34b"):
		return "24 GB"
	case strings.Contains(lower, "70b"):
		return "48 GB"
	default:
		return "8 GB"
	}
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
			tmpl, err := template.ParseFS(web.EmbeddedFS, "templates/"+e.Name())
			if err != nil {
				log.Printf("templates: parse %s: %v", e.Name(), err)
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
	_ = tmpl.Execute(w, data)
}

// pageData extracts PageData from the current request's auth state.
func (s *Server) pageData(r *http.Request) PageData {
	pd := PageData{}
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
		// Check for donor-scoped keys.
		keys, _ := s.store.ListKeys(uid)
		for _, k := range keys {
			if k.Scope == "donor" || k.Scope == "both" {
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
// pageDataWithStats enriches PageData with a registry snapshot for dynamic pages.
func (s *Server) pageDataWithStats(r *http.Request) PageData {
	pd := s.pageData(r)
	snap := s.registry.Snapshot()
	pd.DonorsOnline = snap.DonorsOnline
	pd.ModelsOnline = snap.ModelsOnline
	pd.RequestsToday = int(s.requestsToday)
	pd.TokensToday = s.tokensToday

	// Top models: sort by donor count, limit 5.
	type modelEntry struct {
		name  string
		count int
	}
	var models []modelEntry
	for name, ms := range snap.Models {
		models = append(models, modelEntry{name, ms.DonorsOnline})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].count > models[j].count })
	if len(models) > 5 {
		models = models[:5]
	}
	pd.TopModels = make([]ModelSummary, len(models))
	for i, m := range models {
		pd.TopModels[i] = ModelSummary{Name: m.name, DonorCount: m.count, Vendor: vendorForModel(m.name)}
	}

	// All models for /models page.
	pd.Models = make([]ModelData, 0, len(snap.Models))
	for name, ms := range snap.Models {
		pd.Models = append(pd.Models, ModelData{
			Name:         name,
			DonorsOnline: ms.DonorsOnline,
			Load:         ms.Load,
			Tags:         tagsForModel(name),
			VRAM:         vramForModel(name),
			Vendor:       vendorForModel(name),
		})
	}
	sort.Slice(pd.Models, func(i, j int) bool { return pd.Models[i].Name < pd.Models[j].Name })
	// Default model: most popular (first in TopModels after donor-count sort).
	// Falls back to a widely-used model so the "Try it now" block always renders.
	pd.DefaultModel = "llama3.2:3b"
	if len(pd.TopModels) > 0 {
		pd.DefaultModel = pd.TopModels[0].Name
	}

	return pd
}

