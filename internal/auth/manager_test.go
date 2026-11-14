package auth

import (
	"context"
	"testing"
	"time"
)

func TestManagerAuthenticationAndSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	manager := New("listener", "correct horse battery staple", time.Hour)
	if !manager.Enabled() || !manager.Authenticate("listener", "correct horse battery staple") {
		t.Fatal("valid credentials were rejected")
	}
	if manager.Authenticate("listener", "wrong") || manager.Authenticate("wrong", "correct horse battery staple") {
		t.Fatal("invalid credentials were accepted")
	}
	token, err := manager.CreateSession(ctx)
	valid, validationErr := manager.Valid(ctx, token)
	if err != nil || validationErr != nil || !valid {
		t.Fatalf("CreateSession() token valid = %v, create error = %v, validation error = %v", valid, err, validationErr)
	}
	if err := manager.Revoke(ctx, token); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	valid, err = manager.Valid(ctx, token)
	if err != nil || valid {
		t.Fatal("revoked session remains valid")
	}
}

func TestManagerExpiresSessions(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	manager := New("listener", "password", time.Hour)
	manager.now = func() time.Time { return now }
	token, err := manager.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	now = now.Add(time.Hour)
	valid, err := manager.Valid(ctx, token)
	if err != nil || valid {
		t.Fatal("expired session remains valid")
	}
}

func TestManagerCanBeDisabled(t *testing.T) {
	manager := New("", "", time.Hour)
	if manager.Enabled() || manager.Authenticate("", "") {
		t.Fatal("empty credentials enabled authentication")
	}
}

func TestPersistentManagerSurvivesRecreationAndBindsCredentialsAndSecret(t *testing.T) {
	ctx := context.Background()
	store := newMemorySessionStore()
	secret := "0123456789abcdef0123456789abcdef"
	first, err := NewPersistent("listener", "password", secret, time.Hour, store)
	if err != nil {
		t.Fatalf("NewPersistent() error = %v", err)
	}
	token, err := first.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	restarted, err := NewPersistent("listener", "password", secret, time.Hour, store)
	if err != nil {
		t.Fatalf("restart NewPersistent() error = %v", err)
	}
	valid, err := restarted.Valid(ctx, token)
	if err != nil || !valid {
		t.Fatalf("restarted session valid = %v, error = %v", valid, err)
	}

	changedPassword, _ := NewPersistent("listener", "new-password", secret, time.Hour, store)
	valid, err = changedPassword.Valid(ctx, token)
	if err != nil || valid {
		t.Fatalf("session survived password change: valid=%v error=%v", valid, err)
	}

	secretRotationStore := newMemorySessionStore()
	beforeRotation, _ := NewPersistent("listener", "password", secret, time.Hour, secretRotationStore)
	rotationToken, err := beforeRotation.CreateSession(ctx)
	if err != nil {
		t.Fatalf("create pre-rotation session: %v", err)
	}
	rotatedSecret, _ := NewPersistent("listener", "password", "abcdef0123456789abcdef0123456789", time.Hour, secretRotationStore)
	valid, err = rotatedSecret.Valid(ctx, rotationToken)
	if err != nil || valid {
		t.Fatalf("session survived secret rotation: valid=%v error=%v", valid, err)
	}
}

func TestPersistentManagerRejectsWeakSecretAndMissingStore(t *testing.T) {
	if _, err := NewPersistent("listener", "password", "short", time.Hour, newMemorySessionStore()); err == nil {
		t.Fatal("NewPersistent() accepted a weak secret")
	}
	if _, err := NewPersistent("listener", "password", "0123456789abcdef0123456789abcdef", time.Hour, nil); err == nil {
		t.Fatal("NewPersistent() accepted a missing store")
	}
}
