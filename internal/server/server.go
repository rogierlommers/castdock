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
	"math"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	apidocs "github.com/rlommers/castdock/api"
	authn "github.com/rlommers/castdock/internal/auth"
	contenthtml "github.com/rlommers/castdock/internal/content"
	"github.com/rlommers/castdock/internal/playback"
	"github.com/rlommers/castdock/internal/podcast"
	"github.com/rlommers/castdock/internal/storage"
	"github.com/rlommers/castdock/internal/syndication"
	webassets "github.com/rlommers/castdock/web"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

type Repository interface {
	Health(context.Context) error
	ListFeeds(context.Context) ([]podcast.Feed, error)
	RecentEpisodes(context.Context, int, int) ([]podcast.Episode, error)
	PlayHistoryEpisodes(context.Context, int) ([]podcast.Episode, error)
	InboxEpisodes(context.Context) ([]podcast.Episode, error)
	Feed(context.Context, int64) (podcast.Feed, error)
	EpisodesForFeed(context.Context, int64, int, int) ([]podcast.Episode, error)
	EpisodesByState(context.Context, podcast.EpisodeState, string, int, int) ([]podcast.Episode, error)
	QueuedEpisodes(context.Context) ([]podcast.Episode, error)
	NextQueuedEpisode(context.Context) (podcast.Episode, error)
	Episode(context.Context, int64) (podcast.Episode, error)
	SetEpisodeState(context.Context, int64, podcast.EpisodeState) error
	MoveQueuedEpisode(context.Context, int64, int) error
	ReorderQueuedEpisode(context.Context, int64, int) error
	MarkEpisodePlayed(context.Context, int64) error
	SavePlayback(context.Context, playback.State) (bool, error)
	LibraryStatistics(context.Context) (podcast.LibraryStatistics, error)
	UpdateFeedPlaybackSpeed(context.Context, int64, float64) error
}

type SystemConfiguration struct {
	StartedAt             time.Time
	ListenAddress         string
	DatabasePath          string
	RefreshInterval       time.Duration
	RefreshConcurrency    int
	HTTPTimeout           time.Duration
	MaxFeedBytes          int64
	MaxOPMLBytes          int64
	MaxOPMLFeeds          int
	AllowPrivateFeeds     bool
	LogLevel              string
	AccessLogEnabled      bool
	AuthenticationEnabled bool
	SessionTTL            time.Duration
}

type Options struct {
	MaxOPMLBytes   int64
	MaxOPMLFeeds   int
	AccessLog      bool
	SystemConfig   SystemConfiguration
	Authentication *authn.Manager
}

type Server struct {
	repository     Repository
	syndication    *syndication.Service
	logger         *slog.Logger
	networkEvents  *networkEventStore
	maxOPMLBytes   int64
	maxOPMLFeeds   int
	accessLog      bool
	systemConfig   SystemConfiguration
	authentication *authn.Manager
	templates      map[string]*template.Template
	handler        http.Handler
}

type pageData struct {
	Title          string
	Notice         string
	Error          string
	Feeds          []podcast.Feed
	Feed           podcast.Feed
	Episodes       []podcast.Episode
	InboxEpisodes  []podcast.Episode
	QueuedEpisodes []podcast.Episode
	Episode        podcast.Episode
	Search         string
	ReturnTo       string
	Page           int
	PreviousPage   int
	NextPage       int
	HasPrevious    bool
	HasNext        bool
	Statistics     podcast.LibraryStatistics
	SystemConfig   SystemConfiguration
	AppVersion     string
	GoVersion      string
	Uptime         string
	NetworkEvents  []networkEvent
	AuthEnabled    bool
	LoginError     string
	Next           string
}

func New(repository Repository, service *syndication.Service, logger *slog.Logger, options Options) (*Server, error) {
	templates, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	systemConfig := options.SystemConfig
	if systemConfig.StartedAt.IsZero() {
		systemConfig.StartedAt = time.Now()
	}
	authentication := options.Authentication
	if authentication == nil {
		authentication = authn.New("", "", time.Hour)
	}
	server := &Server{
		repository: repository, syndication: service, logger: logger,
		networkEvents: &networkEventStore{},
		maxOPMLBytes:  options.MaxOPMLBytes, maxOPMLFeeds: options.MaxOPMLFeeds, accessLog: options.AccessLog,
		systemConfig: systemConfig, authentication: authentication, templates: templates,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", server.index)
	mux.HandleFunc("GET /login", server.loginPage)
	mux.HandleFunc("POST /login", server.login)
	mux.HandleFunc("POST /logout", server.logout)
	mux.HandleFunc("POST /api/session", server.createAPISession)
	mux.HandleFunc("DELETE /api/session", server.deleteAPISession)
	mux.HandleFunc("GET /api/home", server.apiHome)
	mux.HandleFunc("GET /api/episodes/{id}", server.apiEpisodeDetail)
	mux.HandleFunc("POST /api/episodes/{id}/queue", server.apiQueueEpisode)
	mux.HandleFunc("POST /api/episodes/{id}/archive", server.apiArchiveEpisode)
	mux.HandleFunc("POST /api/episodes/{id}/played", server.apiMarkEpisodePlayed)
	mux.HandleFunc("POST /api/feeds/{id}/playback-options", server.apiUpdateFeedPlaybackOptions)
	mux.HandleFunc("GET /library", server.library)
	mux.HandleFunc("POST /feeds", server.addFeed)
	mux.HandleFunc("GET /feeds/{id}", server.feed)
	mux.HandleFunc("POST /feeds/{id}/refresh", server.refreshFeed)
	mux.HandleFunc("POST /feeds/{id}/playback-options", server.updateFeedPlaybackOptions)
	mux.HandleFunc("POST /refresh", server.refreshAll)
	mux.HandleFunc("GET /import", server.redirectLegacyImport)
	mux.HandleFunc("POST /import", server.importOPML)
	mux.HandleFunc("POST /library/import", server.importOPML)
	mux.HandleFunc("GET /library/export.opml", server.exportOPML)
	mux.HandleFunc("GET /episodes/{id}", server.episode)
	mux.HandleFunc("POST /episodes/{id}/archive", server.archiveEpisode)
	mux.HandleFunc("POST /episodes/{id}/queue", server.queueEpisode)
	mux.HandleFunc("POST /episodes/{id}/queue/remove", server.removeQueuedEpisode)
	mux.HandleFunc("POST /episodes/{id}/queue/move", server.moveQueuedEpisode)
	mux.HandleFunc("POST /episodes/{id}/played", server.markEpisodePlayed)
	mux.HandleFunc("GET /inbox", server.redirectLegacyInbox)
	mux.HandleFunc("GET /up-next", server.redirectLegacyUpNext)
	mux.HandleFunc("GET /up-next/play", server.playNext)
	mux.HandleFunc("POST /queue/reorder", server.reorderQueue)
	mux.HandleFunc("POST /api/queue/reorder", server.reorderQueue)
	mux.HandleFunc("GET /archive", server.archive)
	mux.HandleFunc("GET /recent", server.recent)
	mux.HandleFunc("GET /history", server.playHistory)
	mux.HandleFunc("GET /system", server.system)
	mux.HandleFunc("POST /api/episodes/{id}/playback", server.savePlayback)
	mux.HandleFunc("GET /healthz", server.health)
	openAPI, err := apidocs.Files.ReadFile("openapi.yaml")
	if err != nil {
		return nil, fmt.Errorf("read embedded OpenAPI document: %w", err)
	}
	mux.HandleFunc("GET /api/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Write(openAPI)
	})
	mux.HandleFunc("GET /swagger", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/swagger/index.html", http.StatusPermanentRedirect)
	})
	mux.Handle("GET /swagger/", httpSwagger.Handler(httpSwagger.URL("/api/openapi.yaml")))
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
		"datetime": func(value *time.Time) string {
			if value == nil {
				return "Never"
			}
			return value.Local().Format("2 Jan 2006, 15:04")
		},
		"datetimeValue": func(value time.Time) string {
			return value.Local().Format("2 Jan 2006, 15:04")
		},
		"duration": func(seconds float64) string {
			total := int(seconds)
			if total >= 3600 {
				return fmt.Sprintf("%d:%02d:%02d", total/3600, total%3600/60, total%60)
			}
			return fmt.Sprintf("%d:%02d", total/60, total%60)
		},
		"queryEscape": url.QueryEscape,
		"relativeTime": func(value *time.Time) string {
			return relativeTime(value, time.Now())
		},
		"relativeTimeValue": func(value time.Time) string {
			return relativeTime(&value, time.Now())
		},
		"showNotesHTML": func(value string) template.HTML {
			return template.HTML(contenthtml.SanitizeShowNotes(value))
		},
		"bytes": humanBytes,
		"speedSelected": func(current, option float64) bool {
			return math.Abs(current-option) < 0.001
		},
	}
	result := make(map[string]*template.Template)
	for _, name := range []string{"index", "library", "feed", "episode", "archive", "recent", "history", "system", "login"} {
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
		response := &responseMetrics{ResponseWriter: w, status: http.StatusOK}
		contentSecurityPolicy := "default-src 'self'; img-src 'self' data: http: https:; media-src http: https:; style-src 'self'; script-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"
		if strings.HasPrefix(r.URL.Path, "/swagger/") {
			contentSecurityPolicy = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"
		}
		response.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		response.Header().Set("Referrer-Policy", "same-origin")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("request panic", "panic", recovered, "method", r.Method, "path", r.URL.Path)
				http.Error(response, "internal server error", http.StatusInternalServerError)
			}
			if s.accessLog {
				s.logger.Info("http request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", response.status,
					"response_bytes", response.bytes,
					"duration", time.Since(started),
				)
			}
			if isClientNetworkRequest(r) {
				s.networkEvents.record(networkEvent{
					OccurredAt: time.Now(),
					Client:     networkEventClient(r), Description: networkEventDescription(r.Method, r.URL.Path),
					Method: r.Method, Path: r.URL.Path, Status: response.status,
				})
			}
		}()
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if !safeWriteRequest(r) {
				s.logger.Debug("cross-origin request rejected",
					"method", r.Method,
					"path", r.URL.Path,
					"request_host", r.Host,
					"origin_host", requestOriginHost(r),
					"sec_fetch_site", strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))),
				)
				http.Error(response, "cross-origin request rejected", http.StatusForbidden)
				return
			}
		}
		if s.authentication.Enabled() && !publicAuthPath(r.URL.Path) && !s.authenticated(r) {
			if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/queue/reorder" {
				http.Error(response, "authentication required", http.StatusUnauthorized)
				return
			}
			next := "/"
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				next = r.URL.RequestURI()
			}
			http.Redirect(response, r, "/login?next="+url.QueryEscape(next), http.StatusSeeOther)
			return
		}
		next.ServeHTTP(response, r)
	})
}

const sessionCookieName = "castdock_session"

func publicAuthPath(path string) bool {
	return path == "/login" || path == "/healthz" || path == "/api/session" || strings.HasPrefix(path, "/static/")
}

func (s *Server) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	return err == nil && s.authentication.Valid(cookie.Value)
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if !s.authentication.Enabled() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if s.authenticated(r) {
		http.Redirect(w, r, safeReturnTo(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	s.render(w, r, "login", pageData{Title: "Sign in", Next: safeReturnTo(r.URL.Query().Get("next"))})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.authentication.Enabled() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	next := safeReturnTo(r.FormValue("next"))
	if !s.authentication.Authenticate(r.FormValue("username"), r.FormValue("password")) {
		time.Sleep(250 * time.Millisecond)
		s.logger.Warn("login failed", "remote_address", remoteHost(r.RemoteAddr))
		s.renderStatus(w, r, "login", pageData{Title: "Sign in", LoginError: "Invalid username or password.", Next: next}, http.StatusUnauthorized)
		return
	}
	token, err := s.authentication.CreateSession()
	if err != nil {
		s.internalError(w, r, fmt.Errorf("create session: %w", err))
		return
	}
	s.setSessionCookie(w, r, token)
	s.logger.Info("login succeeded", "remote_address", remoteHost(r.RemoteAddr))
	http.Redirect(w, r, next, http.StatusSeeOther)
}

type apiSessionRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) createAPISession(w http.ResponseWriter, r *http.Request) {
	if !s.authentication.Enabled() {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	var request apiSessionRequest
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid login payload", http.StatusBadRequest)
		return
	}
	if !s.authentication.Authenticate(request.Username, request.Password) {
		time.Sleep(250 * time.Millisecond)
		s.logger.Warn("API login failed", "remote_address", remoteHost(r.RemoteAddr), "client", r.Header.Get("X-CastDock-Client"))
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}
	token, err := s.authentication.CreateSession()
	if err != nil {
		s.internalError(w, r, fmt.Errorf("create API session: %w", err))
		return
	}
	s.setSessionCookie(w, r, token)
	s.logger.Info("API login succeeded", "remote_address", remoteHost(r.RemoteAddr), "client", r.Header.Get("X-CastDock-Client"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteAPISession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.authentication.Revoke(cookie.Value)
	}
	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.authentication.Revoke(cookie.Value)
	}
	s.clearSessionCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", MaxAge: int(s.authentication.TTL().Seconds()),
		HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func remoteHost(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	return host
}

type responseMetrics struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *responseMetrics) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseMetrics) Write(contents []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	written, err := w.ResponseWriter.Write(contents)
	w.bytes += written
	return written, err
}

func sameOrigin(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case "same-origin":
		return true
	case "cross-site":
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

func safeWriteRequest(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Origin") == "" && strings.TrimSpace(r.Header.Get("X-CastDock-Client")) != "" {
		return true
	}
	return sameOrigin(r)
}

func requestOriginHost(r *http.Request) string {
	source := r.Header.Get("Origin")
	if source == "" {
		source = r.Header.Get("Referer")
	}
	parsed, err := url.Parse(source)
	if err != nil {
		return ""
	}
	return parsed.Host
}

type apiHomeResponse struct {
	Inbox  []apiEpisodeResponse `json:"inbox"`
	UpNext []apiEpisodeResponse `json:"upNext"`
}

type apiEpisodeResponse struct {
	ID              int64                `json:"id"`
	FeedID          int64                `json:"feedId"`
	PodcastTitle    string               `json:"podcastTitle"`
	Title           string               `json:"title"`
	Description     string               `json:"description"`
	PublishedAt     *time.Time           `json:"publishedAt,omitempty"`
	DurationSeconds int                  `json:"durationSeconds"`
	EnclosureURL    string               `json:"enclosureUrl"`
	EnclosureType   string               `json:"enclosureType"`
	ImageURL        string               `json:"imageUrl,omitempty"`
	State           podcast.EpisodeState `json:"state"`
	QueuePosition   *int                 `json:"queuePosition,omitempty"`
	PositionSeconds float64              `json:"positionSeconds"`
	PlayedDuration  float64              `json:"playedDuration"`
	Completed       bool                 `json:"completed"`
	PlaybackUpdated *time.Time           `json:"playbackUpdatedAt,omitempty"`
	PlaybackSpeed   float64              `json:"playbackSpeed"`
}

func newAPIEpisode(episode podcast.Episode) apiEpisodeResponse {
	imageURL := episode.ImageURL
	if imageURL == "" {
		imageURL = episode.FeedImageURL
	}
	return apiEpisodeResponse{
		ID: episode.ID, FeedID: episode.FeedID, PodcastTitle: episode.FeedTitle,
		Title: episode.Title, Description: episode.Description, PublishedAt: episode.PublishedAt,
		DurationSeconds: episode.DurationSeconds, EnclosureURL: episode.EnclosureURL,
		EnclosureType: episode.EnclosureType, ImageURL: imageURL, State: episode.State,
		QueuePosition: episode.QueuePosition, PositionSeconds: episode.PositionSeconds,
		PlayedDuration: episode.PlayedDuration, Completed: episode.Completed,
		PlaybackUpdated: episode.PlaybackUpdated, PlaybackSpeed: episode.FeedPlaybackSpeed,
	}
}

func apiEpisodes(episodes []podcast.Episode) []apiEpisodeResponse {
	result := make([]apiEpisodeResponse, 0, len(episodes))
	for _, episode := range episodes {
		result = append(result, newAPIEpisode(episode))
	}
	return result
}

func (s *Server) apiHome(w http.ResponseWriter, r *http.Request) {
	inbox, err := s.repository.InboxEpisodes(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	queued, err := s.repository.QueuedEpisodes(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.writeJSON(w, apiHomeResponse{Inbox: apiEpisodes(inbox), UpNext: apiEpisodes(queued)})
}

func (s *Server) apiEpisodeDetail(w http.ResponseWriter, r *http.Request) {
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
	s.writeJSON(w, newAPIEpisode(episode))
}

func (s *Server) apiQueueEpisode(w http.ResponseWriter, r *http.Request) {
	s.apiSetEpisodeState(w, r, podcast.EpisodeStateQueued)
}

func (s *Server) apiArchiveEpisode(w http.ResponseWriter, r *http.Request) {
	s.apiSetEpisodeState(w, r, podcast.EpisodeStateArchived)
}

func (s *Server) apiSetEpisodeState(w http.ResponseWriter, r *http.Request, state podcast.EpisodeState) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.repository.SetEpisodeState(r.Context(), id, state); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) apiMarkEpisodePlayed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.repository.MarkEpisodePlayed(r.Context(), id); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type playbackOptionsRequest struct {
	PlaybackSpeed float64 `json:"playbackSpeed"`
}

func (s *Server) apiUpdateFeedPlaybackOptions(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
	decoder.DisallowUnknownFields()
	var request playbackOptionsRequest
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid playback options payload", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid playback options payload", http.StatusBadRequest)
		return
	}
	if !podcast.ValidPlaybackSpeed(request.PlaybackSpeed) {
		http.Error(w, "playback speed must be between 0.5 and 3.0", http.StatusBadRequest)
		return
	}
	if err := s.repository.UpdateFeedPlaybackSpeed(r.Context(), id, request.PlaybackSpeed); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.logger.Debug("feed playback options updated", "feed_id", id, "playback_speed", request.PlaybackSpeed, "client", r.Header.Get("X-CastDock-Client"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		s.logger.Error("write JSON response", "error", err)
	}
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	inboxEpisodes, err := s.repository.InboxEpisodes(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	queuedEpisodes, err := s.repository.QueuedEpisodes(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.render(w, r, "index", pageData{
		InboxEpisodes: inboxEpisodes, QueuedEpisodes: queuedEpisodes,
		Notice: r.URL.Query().Get("notice"), Error: r.URL.Query().Get("error"),
	})
}

func (s *Server) library(w http.ResponseWriter, r *http.Request) {
	feeds, err := s.repository.ListFeeds(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.render(w, r, "library", pageData{
		Title: "Library", Feeds: feeds,
		Notice: r.URL.Query().Get("notice"), Error: r.URL.Query().Get("error"),
	})
}

func (s *Server) addFeed(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.redirectLibrary(w, r, "", "Invalid form submission.")
		return
	}
	feed, created, err := s.syndication.Subscribe(r.Context(), r.FormValue("url"))
	if err != nil {
		if created && feed.ID != 0 {
			http.Redirect(w, r, fmt.Sprintf("/feeds/%d?error=%s", feed.ID, url.QueryEscape("Subscribed, but the first refresh failed: "+err.Error())), http.StatusSeeOther)
			return
		}
		s.redirectLibrary(w, r, "", err.Error())
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
		ReturnTo: r.URL.RequestURI(),
		Notice:   r.URL.Query().Get("notice"), Error: r.URL.Query().Get("error"),
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

func (s *Server) updateFeedPlaybackOptions(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	speed, err := strconv.ParseFloat(r.FormValue("playback_speed"), 64)
	if err != nil || !podcast.ValidPlaybackSpeed(speed) {
		http.Error(w, "playback speed must be between 0.5 and 3.0", http.StatusBadRequest)
		return
	}
	if err := s.repository.UpdateFeedPlaybackSpeed(r.Context(), id, speed); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.logger.Debug("feed playback options updated", "feed_id", id, "playback_speed", speed)
	redirectWithMessage(w, r, fmt.Sprintf("/feeds/%d", id), "notice", fmt.Sprintf("Playback speed set to %gx.", speed))
}

func (s *Server) refreshAll(w http.ResponseWriter, r *http.Request) {
	notice := "Library refresh is already running."
	if s.syndication.StartRefreshAll() {
		notice = "Library refresh started in the background."
	}
	s.redirectLibrary(w, r, notice, "")
}

func (s *Server) redirectLegacyImport(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/library#opml", http.StatusPermanentRedirect)
}

func (s *Server) importOPML(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxOPMLBytes+(1<<20))
	if err := r.ParseMultipartForm(s.maxOPMLBytes); err != nil {
		s.redirectLibrary(w, r, "", "The upload is invalid or exceeds the configured size limit.")
		return
	}
	file, _, err := r.FormFile("opml")
	if err != nil {
		s.redirectLibrary(w, r, "", "Choose an OPML file to import.")
		return
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, s.maxOPMLBytes+1))
	if err != nil || int64(len(contents)) > s.maxOPMLBytes {
		s.redirectLibrary(w, r, "", "The OPML file could not be read or exceeds the configured size limit.")
		return
	}
	urls, err := syndication.ParseOPML(bytes.NewReader(contents))
	if err != nil {
		s.redirectLibrary(w, r, "", err.Error())
		return
	}
	if len(urls) > s.maxOPMLFeeds {
		s.redirectLibrary(w, r, "", fmt.Sprintf("The OPML file contains %d feeds; the configured limit is %d.", len(urls), s.maxOPMLFeeds))
		return
	}
	result := s.syndication.Import(r.Context(), urls)
	notice := fmt.Sprintf("OPML import complete: %d imported, %d skipped, %d refresh failures.", result.Imported, result.Skipped, result.Failed)
	s.redirectLibrary(w, r, notice, "")
}

func (s *Server) exportOPML(w http.ResponseWriter, r *http.Request) {
	feeds, err := s.repository.ListFeeds(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	contents, err := syndication.ExportOPML(feeds)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="castdock-subscriptions-%s.opml"`, time.Now().Format("2006-01-02")))
	w.Write(contents)
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
	s.render(w, r, "episode", pageData{Title: episode.Title, Feed: feed, Episode: episode, ReturnTo: r.URL.RequestURI()})
}

const statePageSize = 100

func (s *Server) redirectLegacyInbox(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/#inbox", http.StatusPermanentRedirect)
}

func (s *Server) archive(w http.ResponseWriter, r *http.Request) {
	s.renderStatePage(w, r, "archive", "Archive", podcast.EpisodeStateArchived, strings.TrimSpace(r.URL.Query().Get("q")))
}

func (s *Server) recent(w http.ResponseWriter, r *http.Request) {
	page, ok := queryPage(w, r)
	if !ok {
		return
	}
	const pageSize = 50
	episodes, err := s.repository.RecentEpisodes(r.Context(), pageSize+1, (page-1)*pageSize)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	hasNext := len(episodes) > pageSize
	if hasNext {
		episodes = episodes[:pageSize]
	}
	s.render(w, r, "recent", pageData{
		Title: "Recent Published", Episodes: episodes, ReturnTo: r.URL.RequestURI(),
		Notice: r.URL.Query().Get("notice"), Error: r.URL.Query().Get("error"),
		Page: page, PreviousPage: page - 1, NextPage: page + 1,
		HasPrevious: page > 1, HasNext: hasNext,
	})
}

func (s *Server) playHistory(w http.ResponseWriter, r *http.Request) {
	episodes, err := s.repository.PlayHistoryEpisodes(r.Context(), 20)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.render(w, r, "history", pageData{
		Title: "Play History", Episodes: episodes, ReturnTo: r.URL.RequestURI(),
		Notice: r.URL.Query().Get("notice"), Error: r.URL.Query().Get("error"),
	})
}

func (s *Server) system(w http.ResponseWriter, r *http.Request) {
	statistics, err := s.repository.LibraryStatistics(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.render(w, r, "system", pageData{
		Title: "System", Statistics: statistics, SystemConfig: s.systemConfig,
		AppVersion: applicationVersion(), GoVersion: runtime.Version(),
		Uptime: formatUptime(time.Since(s.systemConfig.StartedAt)), NetworkEvents: s.networkEvents.recent(),
	})
}

func applicationVersion() string {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok || buildInfo.Main.Version == "" {
		return "development"
	}
	if buildInfo.Main.Version == "(devel)" {
		return "development"
	}
	return buildInfo.Main.Version
}

func formatUptime(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	days := int(value / (24 * time.Hour))
	hours := int(value/time.Hour) % 24
	minutes := int(value/time.Minute) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func humanBytes(value int64) string {
	const unit = int64(1024)
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	amount := float64(value)
	index := -1
	for amount >= float64(unit) && index < len(units)-1 {
		amount /= float64(unit)
		index++
	}
	return fmt.Sprintf("%.1f %s", amount, units[index])
}

func relativeTime(value *time.Time, now time.Time) string {
	if value == nil {
		return "Unknown"
	}
	elapsed := now.Sub(*value)
	if elapsed < 0 {
		elapsed = 0
	}
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		minutes := int(elapsed / time.Minute)
		if minutes == 1 {
			return "one minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	case elapsed < 24*time.Hour:
		hours := int(elapsed / time.Hour)
		if hours == 1 {
			return "one hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case elapsed < 30*24*time.Hour:
		days := int(elapsed / (24 * time.Hour))
		if days == 1 {
			return "one day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return value.Local().Format("2 Jan 2006")
	}
}

func (s *Server) renderStatePage(w http.ResponseWriter, r *http.Request, templateName, title string, state podcast.EpisodeState, search string) {
	page, ok := queryPage(w, r)
	if !ok {
		return
	}
	episodes, err := s.repository.EpisodesByState(r.Context(), state, search, statePageSize+1, (page-1)*statePageSize)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	hasNext := len(episodes) > statePageSize
	if hasNext {
		episodes = episodes[:statePageSize]
	}
	s.render(w, r, templateName, pageData{
		Title: title, Episodes: episodes, Search: search,
		ReturnTo: r.URL.RequestURI(),
		Notice:   r.URL.Query().Get("notice"), Error: r.URL.Query().Get("error"),
		Page: page, PreviousPage: page - 1, NextPage: page + 1,
		HasPrevious: page > 1, HasNext: hasNext,
	})
}

func (s *Server) redirectLegacyUpNext(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/#up-next", http.StatusPermanentRedirect)
}

func (s *Server) playNext(w http.ResponseWriter, r *http.Request) {
	episode, err := s.repository.NextQueuedEpisode(r.Context())
	if errors.Is(err, storage.ErrNotFound) {
		http.Redirect(w, r, "/?notice="+url.QueryEscape("Up Next is empty.")+"#up-next", http.StatusSeeOther)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/episodes/%d", episode.ID), http.StatusSeeOther)
}

func (s *Server) archiveEpisode(w http.ResponseWriter, r *http.Request) {
	s.changeEpisodeState(w, r, podcast.EpisodeStateArchived, "Episode archived.")
}

func (s *Server) queueEpisode(w http.ResponseWriter, r *http.Request) {
	s.changeEpisodeState(w, r, podcast.EpisodeStateQueued, "Episode added to Up Next.")
}

func (s *Server) removeQueuedEpisode(w http.ResponseWriter, r *http.Request) {
	s.changeEpisodeState(w, r, podcast.EpisodeStateArchived, "Episode removed from Up Next and archived.")
}

func (s *Server) changeEpisodeState(w http.ResponseWriter, r *http.Request, state podcast.EpisodeState, notice string) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if err := s.repository.SetEpisodeState(r.Context(), id, state); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.logger.Debug("episode state changed", "episode_id", id, "state", state)
	redirectWithMessage(w, r, safeReturnTo(r.FormValue("return_to")), "notice", notice)
}

func (s *Server) moveQueuedEpisode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	direction := 0
	switch r.FormValue("direction") {
	case "up":
		direction = -1
	case "down":
		direction = 1
	default:
		http.Error(w, "invalid queue direction", http.StatusBadRequest)
		return
	}
	if err := s.repository.MoveQueuedEpisode(r.Context(), id, direction); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.logger.Debug("queue order changed", "episode_id", id, "direction", r.FormValue("direction"))
	redirectWithMessage(w, r, safeReturnTo(r.FormValue("return_to")), "notice", "Queue order updated.")
}

func (s *Server) markEpisodePlayed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if err := s.repository.MarkEpisodePlayed(r.Context(), id); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.logger.Debug("episode marked as played", "episode_id", id)
	redirectWithMessage(w, r, safeReturnTo(r.FormValue("return_to")), "notice", "Episode marked as played and archived.")
}

type queueReorderRequest struct {
	EpisodeID      int64 `json:"episodeId"`
	TargetPosition int   `json:"targetPosition"`
}

func (s *Server) reorderQueue(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
	decoder.DisallowUnknownFields()
	var request queueReorderRequest
	if err := decoder.Decode(&request); err != nil || request.EpisodeID <= 0 || request.TargetPosition <= 0 {
		http.Error(w, "invalid queue reorder payload", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid queue reorder payload", http.StatusBadRequest)
		return
	}
	if err := s.repository.ReorderQueuedEpisode(r.Context(), request.EpisodeID, request.TargetPosition); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, storage.ErrInvalidQueuePosition) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.internalError(w, r, err)
		return
	}
	s.logger.Debug("queue reordered by drag and drop", "episode_id", request.EpisodeID, "target_position", request.TargetPosition)
	w.WriteHeader(http.StatusNoContent)
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
	advanceQueue, err := s.repository.SavePlayback(r.Context(), state)
	if err != nil {
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
	response := struct {
		NextEpisodeURL string `json:"nextEpisodeUrl,omitempty"`
	}{}
	if advanceQueue {
		next, err := s.repository.NextQueuedEpisode(r.Context())
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			s.internalError(w, r, err)
			return
		}
		if err == nil {
			response.NextEpisodeURL = fmt.Sprintf("/episodes/%d", next.ID)
			s.logger.Debug("advancing queue", "completed_episode_id", id, "next_episode_id", next.ID)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("write playback response", "error", err)
	}
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
	s.renderStatus(w, r, name, data, http.StatusOK)
}

func (s *Server) renderStatus(w http.ResponseWriter, r *http.Request, name string, data pageData, status int) {
	data.AuthEnabled = s.authentication.Enabled()
	var output bytes.Buffer
	if err := s.templates[name].ExecuteTemplate(&output, name, data); err != nil {
		s.internalError(w, r, fmt.Errorf("render %s: %w", name, err))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	output.WriteTo(w)
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

func (s *Server) redirectLibrary(w http.ResponseWriter, r *http.Request, notice, message string) {
	query := make(url.Values)
	if notice != "" {
		query.Set("notice", notice)
	}
	if message != "" {
		query.Set("error", message)
	}
	location := "/library"
	if encoded := query.Encode(); encoded != "" {
		location += "?" + encoded
	}
	http.Redirect(w, r, location, http.StatusSeeOther)
}

func queryPage(w http.ResponseWriter, r *http.Request) (int, bool) {
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			http.Error(w, "invalid page", http.StatusBadRequest)
			return 0, false
		}
		page = parsed
	}
	return page, true
}

func safeReturnTo(value string) string {
	if value == "" {
		return "/"
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") {
		return "/"
	}
	return parsed.RequestURI()
}

func redirectWithMessage(w http.ResponseWriter, r *http.Request, location, key, message string) {
	parsed, err := url.Parse(location)
	if err != nil {
		location = "/"
		parsed, _ = url.Parse(location)
	}
	query := parsed.Query()
	query.Set(key, message)
	parsed.RawQuery = query.Encode()
	http.Redirect(w, r, parsed.String(), http.StatusSeeOther)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}
