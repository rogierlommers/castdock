package server

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

const networkEventLimit = 50

type networkEvent struct {
	OccurredAt  time.Time `json:"occurredAt"`
	Client      string    `json:"client"`
	Description string    `json:"description"`
	Method      string    `json:"method"`
	Path        string    `json:"path"`
	Status      int       `json:"status"`
}

type networkEventStore struct {
	mu          sync.RWMutex
	events      []networkEvent
	subscribers map[chan struct{}]struct{}
}

func (s *networkEventStore) record(event networkEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, event)
	if len(s.events) > networkEventLimit {
		copy(s.events, s.events[len(s.events)-networkEventLimit:])
		s.events = s.events[:networkEventLimit]
	}
	for subscriber := range s.subscribers {
		select {
		case subscriber <- struct{}{}:
		default:
		}
	}
}

func (s *networkEventStore) recent() []networkEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	recent := make([]networkEvent, len(s.events))
	for index := range s.events {
		recent[index] = s.events[len(s.events)-1-index]
	}
	return recent
}

func (s *networkEventStore) subscribe() (<-chan struct{}, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subscribers == nil {
		s.subscribers = make(map[chan struct{}]struct{})
	}
	updates := make(chan struct{}, 1)
	s.subscribers[updates] = struct{}{}
	return updates, func() {
		s.mu.Lock()
		delete(s.subscribers, updates)
		s.mu.Unlock()
	}
}

func isClientNetworkRequest(r *http.Request) bool {
	if r.URL.Path == "/api/openapi.yaml" {
		return false
	}
	return strings.HasPrefix(r.URL.Path, "/api/") || strings.TrimSpace(r.Header.Get("X-CastDock-Client")) != ""
}

func networkEventClient(r *http.Request) string {
	client := strings.TrimSpace(r.Header.Get("X-CastDock-Client"))
	if client == "" {
		if r.Header.Get("Sec-Fetch-Site") != "" || r.Header.Get("Origin") != "" || r.Header.Get("Referer") != "" {
			return "web"
		}
		return "unknown"
	}
	client = strings.Join(strings.Fields(client), " ")
	const maxClientLength = 64
	if len(client) > maxClientLength {
		client = client[:maxClientLength]
	}
	return client
}

func networkEventDescription(method, path string) string {
	switch {
	case method == http.MethodPost && path == "/api/session":
		return "Session started"
	case method == http.MethodDelete && path == "/api/session":
		return "Session ended"
	case method == http.MethodGet && path == "/api/home":
		return "Home synchronized"
	case method == http.MethodGet && path == "/healthz":
		return "Health checked"
	case method == http.MethodPost && path == "/api/queue/reorder":
		return "Up Next reordered"
	case method == http.MethodPost && strings.HasSuffix(path, "/playback"):
		return "Playback timestamp saved"
	case method == http.MethodPost && strings.HasSuffix(path, "/queue"):
		return "Episode queued"
	case method == http.MethodPost && strings.HasSuffix(path, "/archive"):
		return "Episode archived"
	case method == http.MethodPost && strings.HasSuffix(path, "/played"):
		return "Episode marked as played"
	case method == http.MethodGet && strings.HasPrefix(path, "/api/episodes/"):
		return "Episode synchronized"
	default:
		return "Client request"
	}
}
