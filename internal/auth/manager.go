package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"sync"
	"time"
)

type Manager struct {
	enabled      bool
	usernameHash [sha256.Size]byte
	passwordHash [sha256.Size]byte
	ttl          time.Duration

	mu       sync.Mutex
	sessions map[string]time.Time
	now      func() time.Time
}

func New(username, password string, ttl time.Duration) *Manager {
	manager := &Manager{ttl: ttl, sessions: make(map[string]time.Time), now: time.Now}
	if username == "" && password == "" {
		return manager
	}
	manager.enabled = true
	manager.usernameHash = sha256.Sum256([]byte(username))
	manager.passwordHash = sha256.Sum256([]byte(password))
	return manager
}

func (m *Manager) Enabled() bool      { return m.enabled }
func (m *Manager) TTL() time.Duration { return m.ttl }

func (m *Manager) Authenticate(username, password string) bool {
	if !m.enabled {
		return false
	}
	usernameHash := sha256.Sum256([]byte(username))
	passwordHash := sha256.Sum256([]byte(password))
	usernameMatches := subtle.ConstantTimeCompare(usernameHash[:], m.usernameHash[:])
	passwordMatches := subtle.ConstantTimeCompare(passwordHash[:], m.passwordHash[:])
	return usernameMatches&passwordMatches == 1
}

func (m *Manager) CreateSession() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeExpiredLocked()
	m.sessions[token] = m.now().Add(m.ttl)
	return token, nil
}

func (m *Manager) Valid(token string) bool {
	if !m.enabled || token == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	expiresAt, ok := m.sessions[token]
	if !ok || !expiresAt.After(m.now()) {
		delete(m.sessions, token)
		return false
	}
	return true
}

func (m *Manager) Revoke(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

func (m *Manager) removeExpiredLocked() {
	now := m.now()
	for token, expiresAt := range m.sessions {
		if !expiresAt.After(now) {
			delete(m.sessions, token)
		}
	}
}
