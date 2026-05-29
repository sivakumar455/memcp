package webui

import (
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sivakumar455/memcp/internal/config"
	"github.com/sivakumar455/memcp/internal/memory"
)

//go:embed templates/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Server holds the dependencies for the web dashboard.
type Server struct {
	cfg   *config.Config
	store *memory.Store
	mux   *http.ServeMux
	tmpl  *template.Template
}

// TemplateData holds the data passed to the partials template.
type TemplateData struct {
	Findings     []FindingView
	Sessions     []*memory.Session
	IdentityText string
}

type FindingView struct {
	Key     string
	Content string
	TimeAgo string
}

// NewServer creates a new web dashboard server.
func NewServer(cfg *config.Config, store *memory.Store) (*Server, error) {
	s := &Server{
		cfg:   cfg,
		store: store,
		mux:   http.NewServeMux(),
	}

	// Parse templates
	tmpl, err := template.ParseFS(templateFS, "templates/index.html", "templates/partials.html")
	if err != nil {
		return nil, err
	}
	s.tmpl = tmpl

	// Register routes
	s.mux.Handle("/static/", http.FileServer(http.FS(staticFS)))
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/api/updates", s.handleUpdates)

	return s, nil
}

// Start runs the server on the specified port.
func (s *Server) Start(addr string) error {
	log.Printf("Web Dashboard running at http://%s", addr)
	return http.ListenAndServe(addr, s.mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	if err := s.tmpl.ExecuteTemplate(w, "index.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleUpdates(w http.ResponseWriter, r *http.Request) {
	data := TemplateData{}

	// 1. Fetch recent findings
	findings, err := s.store.ListFindings(5, 0)
	if err == nil {
		for _, f := range findings {
			content := f.Content
			if len(content) > 100 {
				content = content[:97] + "..."
			}
			data.Findings = append(data.Findings, FindingView{
				Key:     f.Key,
				Content: content,
				TimeAgo: timeSince(f.CreatedAt),
			})
		}
	}

	// 2. Fetch sessions
	sessions, err := s.store.ListSessions()
	if err == nil {
		limit := len(sessions)
		if limit > 5 {
			limit = 5
		}
		data.Sessions = sessions[:limit]
	}

	// 3. Fetch Identity.md
	identityPath := filepath.Join(s.cfg.Persona.SoulDir, "IDENTITY.md")
	contentBytes, err := os.ReadFile(identityPath)
	if err == nil {
		text := string(contentBytes)
		idx := strings.Index(text, "## Learned Patterns")
		if idx != -1 {
			data.IdentityText = text[idx:]
		} else {
			data.IdentityText = text
		}
	} else {
		data.IdentityText = "No learned patterns yet."
	}

	w.Header().Set("Content-Type", "text/html")
	if err := s.tmpl.ExecuteTemplate(w, "updates", data); err != nil {
		log.Printf("Template error: %v", err)
	}
}

func timeSince(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return "a few mins ago"
	}
	return "recently"
}
