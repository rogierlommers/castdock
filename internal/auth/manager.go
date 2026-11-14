package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

const MinimumSessionSecretBytes = 32

type Session struct {
	CredentialHash [sha256.Size]byte
	ExpiresAt      time.Time
}

type SessionStore interface {
	SaveSession(context.Context, [sha256.Size]byte, Session) error
	Session(context.Context, [sha256.Size]byte) (Session, bool, error)
	RevokeSession(context.Context, [sha256.Size]byte) error
	PruneSessions(context.Context, [sha256.Size]byte, time.Time) error
}

type Manager struct {
	enabled        bool
	usernameHash   [sha256.Size]byte
	passwordHash   [sha256.Size]byte
	credentialHash [sha256.Size]byte
	secret         []byte
	ttl            time.Duration
	store          SessionStore
	now            func() time.Time
}

// New creates an in-memory manager for tests and disabled-auth defaults.
// Production authentication uses NewPersistent so sessions survive restarts.
func New(username, password string, ttl time.Duration) *Manager {
	secret := make([]byte, MinimumSessionSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		fallback := sha256.Sum256([]byte(username + "\x00" + password + "\x00castdock-in-memory"))
		secret = fallback[:]
	}
	return newManager(username, password, secret, ttl, newMemorySessionStore())
}

func NewPersistent(username, password, sessionSecret string, ttl time.Duration, store SessionStore) (*Manager, error) {
	if len(sessionSecret) < MinimumSessionSecretBytes {
		return nil, errors.New("session secret must contain at least 32 bytes")
	}
	if store == nil {
		return nil, errors.New("session store is required")
	}
	manager := newManager(username, password, []byte(sessionSecret), ttl, store)
	if manager.enabled {
		if err := store.PruneSessions(context.Background(), manager.credentialHash, manager.now()); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

func newManager(username, password string, secret []byte, ttl time.Duration, store SessionStore) *Manager {
	manager := &Manager{secret: append([]byte(nil), secret...), ttl: ttl, store: store, now: time.Now}
	if username == "" && password == "" {
		return manager
	}
	manager.enabled = true
	manager.usernameHash = sha256.Sum256([]byte(username))
	manager.passwordHash = sha256.Sum256([]byte(password))
	manager.credentialHash = keyedHash(secret, username+"\x00"+password)
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

func (m *Manager) CreateSession(ctx context.Context) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	hash := keyedHash(m.secret, token)
	err := m.store.SaveSession(ctx, hash, Session{
		CredentialHash: m.credentialHash,
		ExpiresAt:      m.now().Add(m.ttl),
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

func (m *Manager) Valid(ctx context.Context, token string) (bool, error) {
	if !m.enabled || token == "" {
		return false, nil
	}
	hash := keyedHash(m.secret, token)
	session, found, err := m.store.Session(ctx, hash)
	if err != nil || !found {
		return false, err
	}
	if !session.ExpiresAt.After(m.now()) {
		return false, m.store.RevokeSession(ctx, hash)
	}
	return subtle.ConstantTimeCompare(session.CredentialHash[:], m.credentialHash[:]) == 1, nil
}

func (m *Manager) Revoke(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return m.store.RevokeSession(ctx, keyedHash(m.secret, token))
}

func keyedHash(secret []byte, value string) [sha256.Size]byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(value))
	var result [sha256.Size]byte
	copy(result[:], mac.Sum(nil))
	return result
}

type memorySessionStore struct {
	mu       sync.Mutex
	sessions map[[sha256.Size]byte]Session
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{sessions: make(map[[sha256.Size]byte]Session)}
}

func (s *memorySessionStore) SaveSession(_ context.Context, tokenHash [sha256.Size]byte, session Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[tokenHash] = session
	return nil
}

func (s *memorySessionStore) Session(_ context.Context, tokenHash [sha256.Size]byte) (Session, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, found := s.sessions[tokenHash]
	return session, found, nil
}

func (s *memorySessionStore) RevokeSession(_ context.Context, tokenHash [sha256.Size]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, tokenHash)
	return nil
}

func (s *memorySessionStore) PruneSessions(_ context.Context, credentialHash [sha256.Size]byte, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for tokenHash, session := range s.sessions {
		credentialsMatch := subtle.ConstantTimeCompare(session.CredentialHash[:], credentialHash[:]) == 1
		if !credentialsMatch || !session.ExpiresAt.After(now) {
			delete(s.sessions, tokenHash)
		}
	}
	return nil
}
