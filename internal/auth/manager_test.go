package auth

import (
	"testing"
	"time"
)

func TestManagerAuthenticationAndSessionLifecycle(t *testing.T) {
	manager := New("listener", "correct horse battery staple", time.Hour)
	if !manager.Enabled() || !manager.Authenticate("listener", "correct horse battery staple") {
		t.Fatal("valid credentials were rejected")
	}
	if manager.Authenticate("listener", "wrong") || manager.Authenticate("wrong", "correct horse battery staple") {
		t.Fatal("invalid credentials were accepted")
	}
	token, err := manager.CreateSession()
	if err != nil || !manager.Valid(token) {
		t.Fatalf("CreateSession() token valid = %v, error = %v", manager.Valid(token), err)
	}
	manager.Revoke(token)
	if manager.Valid(token) {
		t.Fatal("revoked session remains valid")
	}
}

func TestManagerExpiresSessions(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	manager := New("listener", "password", time.Hour)
	manager.now = func() time.Time { return now }
	token, err := manager.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	now = now.Add(time.Hour)
	if manager.Valid(token) {
		t.Fatal("expired session remains valid")
	}
}

func TestManagerCanBeDisabled(t *testing.T) {
	manager := New("", "", time.Hour)
	if manager.Enabled() || manager.Authenticate("", "") {
		t.Fatal("empty credentials enabled authentication")
	}
}
