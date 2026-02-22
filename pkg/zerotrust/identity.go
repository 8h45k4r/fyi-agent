// Package zerotrust implements Zero Trust identity verification and
// session management for the FYI Agent. Supports OIDC, SAML, and
// certificate-based authentication with automatic token refresh.
package zerotrust

import (
	"context"
	"crypto/x509"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// AuthMethod represents the authentication method.
type AuthMethod string

const (
	AuthOIDC        AuthMethod = "oidc"
	AuthSAML        AuthMethod = "saml"
	AuthCertificate AuthMethod = "certificate"
)

// Config holds identity provider configuration.
type Config struct {
	Provider             AuthMethod    `yaml:"provider"`
	IssuerURL            string        `yaml:"issuer_url"`
	ClientID             string        `yaml:"client_id"`
	ClientSecret         string        `yaml:"client_secret"`
	TokenRefreshInterval time.Duration `yaml:"token_refresh_interval"`
	CertFile             string        `yaml:"cert_file"`
	KeyFile              string        `yaml:"key_file"`
}

// DefaultConfig returns sensible identity defaults.
func DefaultConfig() Config {
	return Config{
		Provider:             AuthOIDC,
		TokenRefreshInterval: 5 * time.Minute,
	}
}

// Identity represents a verified user/device identity.
type Identity struct {
	Subject     string            `json:"sub"`
	Email       string            `json:"email,omitempty"`
	DeviceID    string            `json:"device_id,omitempty"`
	Groups      []string          `json:"groups,omitempty"`
	Claims      map[string]string `json:"claims,omitempty"`
	IssuedAt    time.Time         `json:"iat"`
	ExpiresAt   time.Time         `json:"exp"`
	Certificate *x509.Certificate `json:"-"`
}

// IsExpired returns true if the identity token has expired.
func (id *Identity) IsExpired() bool {
	if id == nil {
		return true
	}
	return time.Now().After(id.ExpiresAt)
}

// IsValid checks if the identity is non-nil and not expired.
func (id *Identity) IsValid() bool {
	return id != nil && !id.IsExpired() && id.Subject != ""
}

// TimeToExpiry returns the duration until token expiry.
func (id *Identity) TimeToExpiry() time.Duration {
	if id == nil {
		return 0
	}
	d := time.Until(id.ExpiresAt)
	if d < 0 {
		return 0
	}
	return d
}

// Provider defines the interface for identity providers.
type Provider interface {
	// Authenticate performs initial authentication.
	Authenticate(ctx context.Context) (*Identity, error)
	// Refresh refreshes an existing identity token.
	Refresh(ctx context.Context, current *Identity) (*Identity, error)
	// Revoke invalidates the current identity.
	Revoke(ctx context.Context, current *Identity) error
}

// Manager manages identity lifecycle including authentication and refresh.
type Manager struct {
	cfg      Config
	logger   *slog.Logger
	provider Provider
	current  *Identity
	mu       sync.RWMutex
	stopCh   chan struct{}
}

// NewManager creates a new identity manager.
func NewManager(cfg Config, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		cfg:    cfg,
		logger: logger,
		stopCh: make(chan struct{}),
	}
}

// SetProvider sets the identity provider implementation.
func (m *Manager) SetProvider(p Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider = p
}

// Authenticate performs initial authentication.
func (m *Manager) Authenticate(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.provider == nil {
		return fmt.Errorf("zerotrust: no provider configured")
	}

	id, err := m.provider.Authenticate(ctx)
	if err != nil {
		return fmt.Errorf("zerotrust: authentication failed: %w", err)
	}

	m.current = id
	m.logger.Info("authenticated",
		"subject", id.Subject,
		"expires_in", id.TimeToExpiry().String(),
	)
	return nil
}

// CurrentIdentity returns the current identity (may be nil or expired).
func (m *Manager) CurrentIdentity() *Identity {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// IsAuthenticated returns true if there is a valid, non-expired identity.
func (m *Manager) IsAuthenticated() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.IsValid()
}

// StartRefreshLoop starts a background token refresh loop.
func (m *Manager) StartRefreshLoop(ctx context.Context) error {
	if m.cfg.TokenRefreshInterval <= 0 {
		return fmt.Errorf("zerotrust: invalid refresh interval")
	}

	ticker := time.NewTicker(m.cfg.TokenRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.stopCh:
			return nil
		case <-ticker.C:
			m.refreshToken(ctx)
		}
	}
}

// Stop signals the refresh loop to stop.
func (m *Manager) Stop() {
	select {
	case m.stopCh <- struct{}{}:
	default:
	}
}

func (m *Manager) refreshToken(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.provider == nil || m.current == nil {
		return
	}

	// Refresh if within 20% of expiry
	ttl := m.current.TimeToExpiry()
	threshold := time.Duration(float64(m.cfg.TokenRefreshInterval) * 0.8)
	if ttl > threshold {
		return
	}

	newID, err := m.provider.Refresh(ctx, m.current)
	if err != nil {
		m.logger.Error("token refresh failed", "error", err)
		return
	}

	m.current = newID
	m.logger.Info("token refreshed",
		"subject", newID.Subject,
		"expires_in", newID.TimeToExpiry().String(),
	)
}
