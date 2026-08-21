package webui

import (
	"embed"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"tiktok-agent/internal/status"
)

//go:embed templates/*.html
var templateFS embed.FS

// Server は WebUI の HTTP サーバ。
type Server struct {
	store  *status.Store
	logger *slog.Logger
	tmpl   *template.Template
}

// New は Server を生成する。
func New(store *status.Store, logger *slog.Logger) (*Server, error) {
	tmpl, err := template.New("index.html").Funcs(template.FuncMap{
		"stdout": func(c status.Command) string {
			if c.Stdout == nil {
				return ""
			}
			return strings.Join(c.Stdout.Snapshot(), "\n")
		},
		"stderr": func(c status.Command) string {
			if c.Stderr == nil {
				return ""
			}
			return strings.Join(c.Stderr.Snapshot(), "\n")
		},
	}).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{store: store, logger: logger, tmpl: tmpl}, nil
}

type indexData struct {
	Lives    []status.Live
	Commands []status.Command
	Logs     []string
}

// Handler はルーティング済みの HTTP ハンドラを返す。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/lives", s.handleLives)
	mux.HandleFunc("/api/commands", s.handleCommands)
	mux.HandleFunc("/api/logs", s.handleLogs)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data := indexData{
		Lives:    s.store.Lives(),
		Commands: s.store.Commands(),
		Logs:     s.store.LogsLast(100),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.Execute(w, data); err != nil {
		s.logger.Error("template execute failed", "error", err)
	}
}

func (s *Server) handleLives(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.store.Lives())
}

func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.store.Commands())
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.store.Logs())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
