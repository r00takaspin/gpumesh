package coord

import (
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/gpumesh/gpumesh/web"
)

// PageData is passed to every template render.
type PageData struct {
	LoggedIn bool
	Login    string
}

var (
	templates     map[string]*template.Template
	templatesOnce sync.Once
)

func getTemplates() map[string]*template.Template {
	templatesOnce.Do(func() {
		templates = make(map[string]*template.Template)
		entries, err := web.TemplatesFS.ReadDir("templates")
		if err != nil {
			log.Printf("templates: readdir: %v", err)
			return
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".html" {
				continue
			}
			tmpl, err := template.ParseFS(web.TemplatesFS, "templates/"+e.Name())
			if err != nil {
				log.Printf("templates: parse %s: %v", e.Name(), err)
				continue
			}
			templates[e.Name()] = tmpl
		}
	})
	return templates
}

func renderTemplate(w http.ResponseWriter, name string, data PageData) {
	tmpl, ok := getTemplates()[name]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	tmpl.Execute(w, data)
}

// pageData extracts PageData from the current request's auth state.
func (s *Server) pageData(r *http.Request) PageData {
	pd := PageData{}
	cookie, err := r.Cookie("gpumesh_session")
	if err == nil {
		uid, err := s.store.ValidateSession(cookie.Value)
		if err == nil && uid != 0 {
			pd.LoggedIn = true
			pd.Login, _ = s.store.GetUserByID(uid)
		}
	}
	return pd
}
