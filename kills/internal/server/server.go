package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"kills/internal/config"
	"kills/internal/killer"
	"kills/internal/processlist"
)

//go:embed web/*
var webFS embed.FS

type Server struct {
	store   *config.Store
	baseURL func() string
}

func New(store *config.Store, baseURL func() string) *Server {
	return &Server{store: store, baseURL: baseURL}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/kill", s.handleKill)
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/info", s.handleInfo)

	webRoot, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(webRoot)))
	return mux
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := s.store.Get()
	writeJSON(w, map[string]any{
		"url":  s.baseURL(),
		"port": cfg.Port,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.store.Get())
	case http.MethodPut:
		var cfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := s.store.Replace(cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, s.store.Get())
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type killRequest struct {
	ProfileID string `json:"profileId"`
}

func (s *Server) handleKill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req killRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	cfg := s.store.Get()
	profileID := req.ProfileID
	if profileID == "" {
		profileID = cfg.ActiveProfileID
	}

	var names []string
	for _, p := range cfg.Profiles {
		if p.ID == profileID {
			names = p.Processes
			break
		}
	}

	// Also accept newline-separated text from textarea if stored as single string slice
	var expanded []string
	for _, line := range names {
		for _, part := range strings.Split(line, "\n") {
			part = strings.TrimSpace(part)
			if part != "" {
				expanded = append(expanded, part)
			}
		}
	}

	results := killer.KillAll(expanded)
	writeJSON(w, map[string]any{
		"summary": killer.Summary(results),
		"results": results,
	})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	items, err := processlist.Search(q, 80)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"items": items})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
