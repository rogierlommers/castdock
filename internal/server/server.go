package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rlommers/castdock/internal/playback"
	"github.com/rlommers/castdock/internal/podcast"
	"github.com/rlommers/castdock/internal/storage"
	"github.com/rlommers/castdock/internal/syndication"
	webassets "github.com/rlommers/castdock/web"
)

type Repository interface {
	Health(context.Context) error
	ListFeeds(context.Context) ([]podcast.Feed, error)
	RecentEpisodes(context.Context, int) ([]podcast.Episode, error)
	Feed(context.Context, int64) (podcast.Feed, error)
	EpisodesForFeed(context.Context, int64, int, int) ([]podcast.Episode, error)
	Episode(context.Context, int64) (podcast.Episode, error)
	SavePlayback(context.Context, playback.State) error
}

type Server struct {
	repository   Repository
	syndication  *syndication.Service
	logger       *slog.Logger
	maxOPMLBytes int64
	maxOPMLFeeds int
	templates    map[string]*template.Template
	handler      http.Handler
}

type pageData struct {
	Title        string
	Notice       string
	Error        string
	Feeds        []podcast.Feed
	Feed         podcast.Feed
	Episodes     []podcast.Episode
	Episode      podcast.Episode
	Result       *syndication.ImportResult
	Page         int
	PreviousPage int
	NextPage     int
	HasPrevious  bool
	HasNext      bool
}

func New(repository Repository, service *syndication.Service, logger *slog.Logger, maxOPMLBytes int64, maxOPMLFeeds int) (*Server, error) {
	templates, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	server := &Server{
		repository: repository, syndication: service, logger: logger,
		maxOPMLBytes: maxOPMLBytes, maxOPMLFeeds: maxOPMLFeeds, templates: templates,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", server.index)
	mux.HandleFunc("POST /feeds", server.addFeed)
	mux.HandleFunc("GET /feeds/{id}", server.feed)
	mux.HandleFunc("POST /feeds/{id}/refresh", server.refreshFeed)
	mux.HandleFunc("POST /refresh", server.refreshAll)
	mux.HandleFunc("GET /import", server.importPage)
	mux.HandleFunc("POST /import", server.importOPML)
	mux.HandleFunc("GET /episodes/{id}", server.episode)
	mux.HandleFunc("POST /api/episodes/{id}/playback", server.savePlayback)
	mux.HandleFunc("GET /healthz", server.health)
	staticFS, err := fs.Sub(webassets.Files, "static")
	if err != nil {
		return nil, fmt.Errorf("open static assets: %w", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	server.handler = server.middleware(mux)
	return server, nil
}

func (s *Server) Handler() http.Handler { return s.handler }

func parseTemplates() (map[string]*template.Template, error) {
	functions := template.FuncMap{
		"date": func(value *time.Time) string {
			if value == nil {
				return ""
			}
			return value.Local().Format("2 Jan 2006")
		},
		"duration": func(seconds float64) string {
			total := int(seconds)
			if total >= 3600 {
				return fmt.Sprintf("%d:%02d:%02d", total/3600, total%3600/60, total%60)
			}
			return fmt.Sprintf("%d:%02d", total/60, total%60)
		},
	}
	result := make(map[string]*template.Template)
	for _, name := range []string{"index", "feed", "episode", "import"} {
		parsed, err := template.New(name).Funcs(functions).ParseFS(
			webassets.Files, "templates/layout.html", "templates/"+name+".html",
		)
		if err != nil {
			return nil, fmt.Errorf("parse %s template: %w", name, err)
		}
		result[name] = parsed
	}
	return result, nil
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: http: https:; media-src http: https:; style-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if !sameOrigin(r) {
				http.Error(w, "cross-origin request rejected", http.StatusForbidden)
				return
			}
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("request panic", "panic", recovered, "method", r.Method, "path", r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
			s.logger.Debug("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started))
		}()
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case "same-origin":
		return true
	case "same-site", "cross-site":
		return false
	}
	source := r.Header.Get("Origin")
	if source == "" {
		source = r.Header.Get("Referer")
	}
	if source == "" {
		return false
	}
	parsed, err := url.Parse(source)
	return err == nil && strings.EqualFold(parsed.Host, r.Host)
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	feeds, err := s.repository.ListFeeds(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	episodes, err := s.repository.RecentEpisodes(r.Context(), 20)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.render(w, r, "index", pageData{Feeds: feeds, Episodes: episodes, Notice: r.URL.Query().Get("notice"), Error: r.URL.Query().Get("error")})
}

func (s *Server) addFeed(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.redirectHome(w, r, "", "Invalid form submission.")
		return
	}
	feed, created, err := s.syndication.Subscribe(r.Context(), r.FormValue("url"))
	if err != nil {
		if created && feed.ID != 0 {
			http.Redirect(w, r, fmt.Sprintf("/feeds/%d?error=%s", feed.ID, url.QueryEscape("Subscribed, but the first refresh failed: "+err.Error())), http.StatusSeeOther)
			return
		}
		s.redirectHome(w, r, "", err.Error())
		return
	}
	if !created {
		http.Redirect(w, r, fmt.Sprintf("/feeds/%d?notice=%s", feed.ID, url.QueryEscape("Already subscribed.")), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/feeds/%d?notice=%s", feed.ID, url.QueryEscape("Podcast subscribed and refreshed.")), http.StatusSeeOther)
}

func (s *Server) feed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	feed, err := s.repository.Feed(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 {
			http.Error(w, "invalid page", http.StatusBadRequest)
			return
		}
		page = parsed
	}
	const pageSize = 100
	episodes, err := s.repository.EpisodesForFeed(r.Context(), id, pageSize, (page-1)*pageSize)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.render(w, r, "feed", pageData{
		Title: feed.Title, Feed: feed, Episodes: episodes,
		Notice: r.URL.Query().Get("notice"), Error: r.URL.Query().Get("error"),
		Page: page, PreviousPage: page - 1, NextPage: page + 1,
		HasPrevious: page > 1, HasNext: page*pageSize < feed.EpisodeCount,
	})
}

func (s *Server) refreshFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := s.syndication.Refresh(r.Context(), id)
	location := fmt.Sprintf("/feeds/%d", id)
	if err != nil {
		location += "?error=" + url.QueryEscape("Refresh failed: "+err.Error())
	} else {
		location += "?notice=" + url.QueryEscape("Feed refreshed.")
	}
	http.Redirect(w, r, location, http.StatusSeeOther)
}

func (s *Server) refreshAll(w http.ResponseWriter, r *http.Request) {
	result := s.syndication.RefreshAll(r.Context())
	notice := fmt.Sprintf("Refreshed %d feeds; %d failed.", result.Refreshed, result.Failed)
	s.redirectHome(w, r, notice, "")
}

func (s *Server) importPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "import", pageData{Title: "Import OPML"})
}

func (s *Server) importOPML(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxOPMLBytes+(1<<20))
	if err := r.ParseMultipartForm(s.maxOPMLBytes); err != nil {
		s.render(w, r, "import", pageData{Title: "Import OPML", Error: "The upload is invalid or exceeds the configured size limit."})
		return
	}
	file, _, err := r.FormFile("opml")
	if err != nil {
		s.render(w, r, "import", pageData{Title: "Import OPML", Error: "Choose an OPML file to import."})
		return
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, s.maxOPMLBytes+1))
	if err != nil || int64(len(contents)) > s.maxOPMLBytes {
		s.render(w, r, "import", pageData{Title: "Import OPML", Error: "The OPML file could not be read or exceeds the configured size limit."})
		return
	}
	urls, err := syndication.ParseOPML(bytes.NewReader(contents))
	if err != nil {
		s.render(w, r, "import", pageData{Title: "Import OPML", Error: err.Error()})
		return
	}
	if len(urls) > s.maxOPMLFeeds {
		s.render(w, r, "import", pageData{Title: "Import OPML", Error: fmt.Sprintf("The OPML file contains %d feeds; the configured limit is %d.", len(urls), s.maxOPMLFeeds)})
		return
	}
	result := s.syndication.Import(r.Context(), urls)
	s.render(w, r, "import", pageData{Title: "Import OPML", Result: &result})
}

func (s *Server) episode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	episode, err := s.repository.Episode(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	feed, err := s.repository.Feed(r.Context(), episode.FeedID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.render(w, r, "episode", pageData{Title: episode.Title, Feed: feed, Episode: episode})
}

type playbackRequest struct {
	Position        float64   `json:"position"`
	Duration        float64   `json:"duration"`
	Completed       bool      `json:"completed"`
	Playing         bool      `json:"playing"`
	ClientUpdatedAt time.Time `json:"clientUpdatedAt"`
}

func (s *Server) savePlayback(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
	decoder.DisallowUnknownFields()
	var request playbackRequest
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid playback payload", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid playback payload", http.StatusBadRequest)
		return
	}
	now := time.Now()
	if request.ClientUpdatedAt.IsZero() || request.ClientUpdatedAt.Before(now.Add(-10*time.Minute)) || request.ClientUpdatedAt.After(now.Add(10*time.Minute)) {
		http.Error(w, "invalid client timestamp", http.StatusBadRequest)
		return
	}
	state := playback.State{
		EpisodeID: id, Position: request.Position, Duration: request.Duration,
		Completed: request.Completed, Playing: request.Playing, ClientUpdatedAt: request.ClientUpdatedAt,
	}
	if err := s.repository.SavePlayback(r.Context(), state); err != nil {
		if errors.Is(err, playback.ErrInvalidEpisode) || errors.Is(err, playback.ErrInvalidPosition) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.logger.Debug("playback position saved",
		"episode_id", id,
		"position_seconds", request.Position,
		"duration_seconds", request.Duration,
		"completed", request.Completed,
		"playing", request.Playing,
		"client_updated_at", request.ClientUpdatedAt,
	)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.repository.Health(r.Context()); err != nil {
		http.Error(w, "unhealthy", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok\n"))
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data pageData) {
	var output bytes.Buffer
	if err := s.templates[name].ExecuteTemplate(&output, name, data); err != nil {
		s.internalError(w, r, fmt.Errorf("render %s: %w", name, err))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	output.WriteTo(w)
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

func (s *Server) redirectHome(w http.ResponseWriter, r *http.Request, notice, message string) {
	query := make(url.Values)
	if notice != "" {
		query.Set("notice", notice)
	}
	if message != "" {
		query.Set("error", message)
	}
	location := "/"
	if encoded := query.Encode(); encoded != "" {
		location += "?" + encoded
	}
	http.Redirect(w, r, location, http.StatusSeeOther)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}
