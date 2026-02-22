package zerotrust

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Provider != AuthOIDC {
		t.Errorf("expected AuthOIDC provider, got %q", cfg.Provider)
	}
	if cfg.TokenRefreshInterval != 5*time.Minute {
		t.Errorf("expected 5m refresh interval, got %v", cfg.TokenRefreshInterval)
	}
}

func TestIdentityIsExpired(t *testing.T) {
	// Expired identity
	id := &Identity{
		Subject:   "user@example.com",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	if !id.IsExpired() {
		t.Error("identity should be expired")
	}

	// Valid identity
	id2 := &Identity{
		Subject:   "user@example.com",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if id2.IsExpired() {
		t.Error("identity should not be expired")
	}
}

func TestIdentityIsExpiredNil(t *testing.T) {
	var id *Identity
	if !id.IsExpired() {
		t.Error("nil identity should be expired")
	}
}

func TestIdentityIsValid(t *testing.T) {
	// Valid identity
	id := &Identity{
		Subject:   "user@example.com",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if !id.IsValid() {
		t.Error("identity should be valid")
	}

	// Expired
	id.ExpiresAt = time.Now().Add(-1 * time.Hour)
	if id.IsValid() {
		t.Error("expired identity should not be valid")
	}

	// Empty subject
	id2 := &Identity{
		Subject:   "",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if id2.IsValid() {
		t.Error("identity with empty subject should not be valid")
	}
}

func TestIdentityIsValidNil(t *testing.T) {
	var id *Identity
	if id.IsValid() {
		t.Error("nil identity should not be valid")
	}
}

func TestIdentityTimeToExpiry(t *testing.T) {
	id := &Identity{
		Subject:   "user@example.com",
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	ttl := id.TimeToExpiry()
	if ttl < 29*time.Minute || ttl > 31*time.Minute {
		t.Errorf("expected ~30m TTL, got %v", ttl)
	}
}

func TestIdentityTimeToExpiryExpired(t *testing.T) {
	id := &Identity{
		Subject:   "user@example.com",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	ttl := id.TimeToExpiry()
	if ttl != 0 {
		t.Errorf("expired identity TTL should be 0, got %v", ttl)
	}
}

func TestIdentityTimeToExpiryNil(t *testing.T) {
	var id *Identity
	ttl := id.TimeToExpiry()
	if ttl != 0 {
		t.Errorf("nil identity TTL should be 0, got %v", ttl)
	}
}

func TestNewManager(t *testing.T) {
	cfg := DefaultConfig()
	m := NewManager(cfg, nil)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
}

func TestManagerIsAuthenticated(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	if m.IsAuthenticated() {
		t.Error("should not be authenticated initially")
	}
}

func TestManagerCurrentIdentity(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	if m.CurrentIdentity() != nil {
		t.Error("current identity should be nil initially")
	}
}

func TestManagerAuthenticateNoProvider(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	err := m.Authenticate(context.Background())
	if err == nil {
		t.Error("authenticate with no provider should fail")
	}
}

// mockProvider implements Provider for testing.
type mockProvider struct {
	authID  *Identity
	authErr error
}

func (p *mockProvider) Authenticate(ctx context.Context) (*Identity, error) {
	if p.authErr != nil {
		return nil, p.authErr
	}
	return p.authID, nil
}

func (p *mockProvider) Refresh(ctx context.Context, current *Identity) (*Identity, error) {
	return p.authID, nil
}

func (p *mockProvider) Revoke(ctx context.Context, current *Identity) error {
	return nil
}

func TestManagerAuthenticateWithProvider(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	m.SetProvider(&mockProvider{
		authID: &Identity{
			Subject:   "test-user",
			ExpiresAt: time.Now().Add(1 * time.Hour),
		},
	})

	err := m.Authenticate(context.Background())
	if err != nil {
		t.Fatalf("Authenticate returned error: %v", err)
	}
	if !m.IsAuthenticated() {
		t.Error("should be authenticated after successful auth")
	}
	if m.CurrentIdentity().Subject != "test-user" {
		t.Errorf("expected subject 'test-user', got %q", m.CurrentIdentity().Subject)
	}
}

func TestManagerAuthenticateError(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	m.SetProvider(&mockProvider{
		authErr: fmt.Errorf("auth failed"),
	})

	err := m.Authenticate(context.Background())
	if err == nil {
		t.Error("expected error from failed authentication")
	}
}

func TestAuthMethodConstants(t *testing.T) {
	if AuthOIDC != "oidc" {
		t.Error("AuthOIDC should be 'oidc'")
	}
	if AuthSAML != "saml" {
		t.Error("AuthSAML should be 'saml'")
	}
	if AuthCertificate != "certificate" {
		t.Error("AuthCertificate should be 'certificate'")
	}
}
