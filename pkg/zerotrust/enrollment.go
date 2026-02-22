package zerotrust

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// EnrollmentState represents the current enrollment status.
type EnrollmentState int

const (
	EnrollmentStateUnenrolled EnrollmentState = iota
	EnrollmentStatePending
	EnrollmentStateEnrolled
	EnrollmentStateExpired
	EnrollmentStateRevoked
)

// String returns the string representation of enrollment state.
func (s EnrollmentState) String() string {
	switch s {
	case EnrollmentStateUnenrolled:
		return "unenrolled"
	case EnrollmentStatePending:
		return "pending"
	case EnrollmentStateEnrolled:
		return "enrolled"
	case EnrollmentStateExpired:
		return "expired"
	case EnrollmentStateRevoked:
		return "revoked"
	default:
		return "unknown"
	}
}

// EnrollmentToken represents a one-time enrollment token from the controller.
type EnrollmentToken struct {
	Token      string `json:"token"`
	Controller string `json:"controller"`
	Method     string `json:"method"`
	IssuedAt   int64  `json:"iat"`
	ExpiresAt  int64  `json:"exp"`
}

// IsExpired checks if the enrollment token has expired.
func (t *EnrollmentToken) IsExpired() bool {
	if t.ExpiresAt == 0 {
		return false
	}
	return time.Now().Unix() > t.ExpiresAt
}

// EnrollmentResult contains the output of a successful enrollment.
type EnrollmentResult struct {
	IdentityID  string    `json:"identity_id"`
	CertPath    string    `json:"cert_path"`
	KeyPath     string    `json:"key_path"`
	CAPath      string    `json:"ca_path"`
	ConfigPath  string    `json:"config_path"`
	EnrolledAt  time.Time `json:"enrolled_at"`
	Controller  string    `json:"controller"`
}

// EnrollmentConfig holds configuration for the enrollment process.
type EnrollmentConfig struct {
	// TokenPath is the path to the JWT enrollment token file.
	TokenPath string `yaml:"token_path" json:"token_path"`
	// IdentityDir is the directory to store identity credentials.
	IdentityDir string `yaml:"identity_dir" json:"identity_dir"`
	// Controller is the OpenZiti controller URL.
	Controller string `yaml:"controller" json:"controller"`
	// CACert is an optional path to a custom CA certificate.
	CACert string `yaml:"ca_cert" json:"ca_cert"`
	// AutoRenew enables automatic certificate renewal.
	AutoRenew bool `yaml:"auto_renew" json:"auto_renew"`
	// RenewalThreshold is how far before expiry to trigger renewal.
	RenewalThreshold time.Duration `yaml:"renewal_threshold" json:"renewal_threshold"`
}

// DefaultEnrollmentConfig returns sensible defaults.
func DefaultEnrollmentConfig() EnrollmentConfig {
	return EnrollmentConfig{
		IdentityDir:      "/var/lib/fyi-agent/identity",
		AutoRenew:        true,
		RenewalThreshold: 72 * time.Hour, // 3 days before expiry
	}
}

// Enrollor handles OpenZiti identity enrollment and credential management.
type Enrollor struct {
	mu       sync.RWMutex
	config   EnrollmentConfig
	state    EnrollmentState
	result   *EnrollmentResult
	logger   *slog.Logger
	stopCh   chan struct{}
	running  bool
}

// NewEnrollor creates a new enrollment manager.
func NewEnrollor(config EnrollmentConfig, logger *slog.Logger) *Enrollor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Enrollor{
		config: config,
		state:  EnrollmentStateUnenrolled,
		logger: logger.With("component", "enrollment"),
		stopCh: make(chan struct{}),
	}
}

// State returns the current enrollment state.
func (e *Enrollor) State() EnrollmentState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state
}

// Result returns the enrollment result if enrolled.
func (e *Enrollor) Result() *EnrollmentResult {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.result
}

// Enroll performs the enrollment process using a JWT token.
func (e *Enrollor) Enroll(tokenData []byte) (*EnrollmentResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.state = EnrollmentStatePending
	e.logger.Info("starting enrollment")

	// Parse the enrollment token.
	var token EnrollmentToken
	if err := json.Unmarshal(tokenData, &token); err != nil {
		e.state = EnrollmentStateUnenrolled
		return nil, fmt.Errorf("invalid enrollment token: %w", err)
	}

	if token.IsExpired() {
		e.state = EnrollmentStateExpired
		return nil, fmt.Errorf("enrollment token has expired")
	}

	// Ensure identity directory exists.
	if err := os.MkdirAll(e.config.IdentityDir, 0700); err != nil {
		e.state = EnrollmentStateUnenrolled
		return nil, fmt.Errorf("failed to create identity directory: %w", err)
	}

	// Perform the enrollment (connect to controller, get certs).
	result, err := e.performEnrollment(&token)
	if err != nil {
		e.state = EnrollmentStateUnenrolled
		return nil, fmt.Errorf("enrollment failed: %w", err)
	}

	e.state = EnrollmentStateEnrolled
	e.result = result

	e.logger.Info("enrollment completed",
		"identity_id", result.IdentityID,
		"controller", result.Controller,
	)

	return result, nil
}

// EnrollFromFile reads a token file and performs enrollment.
func (e *Enrollor) EnrollFromFile(tokenPath string) (*EnrollmentResult, error) {
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}
	return e.Enroll(data)
}

// StartAutoRenewal begins monitoring certificate expiry and auto-renewing.
func (e *Enrollor) StartAutoRenewal() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.config.AutoRenew {
		return fmt.Errorf("auto-renewal is disabled")
	}

	if e.running {
		return fmt.Errorf("auto-renewal already running")
	}

	e.running = true
	go e.renewalLoop()
	e.logger.Info("auto-renewal started",
		"threshold", e.config.RenewalThreshold,
	)
	return nil
}

// StopAutoRenewal stops the auto-renewal loop.
func (e *Enrollor) StopAutoRenewal() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return
	}

	close(e.stopCh)
	e.running = false
	e.logger.Info("auto-renewal stopped")
}

// CertificateExpiry returns when the current identity certificate expires.
func (e *Enrollor) CertificateExpiry() (time.Time, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.result == nil {
		return time.Time{}, fmt.Errorf("not enrolled")
	}

	certData, err := os.ReadFile(e.result.CertPath)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to read certificate: %w", err)
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		return time.Time{}, fmt.Errorf("failed to decode PEM block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return cert.NotAfter, nil
}

// Revoke marks the identity as revoked and cleans up credentials.
func (e *Enrollor) Revoke() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.state != EnrollmentStateEnrolled {
		return fmt.Errorf("cannot revoke: not enrolled")
	}

	// Clean up credential files.
	if e.result != nil {
		for _, path := range []string{e.result.CertPath, e.result.KeyPath} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				e.logger.Warn("failed to remove credential file",
					"path", path,
					"error", err,
				)
			}
		}
	}

	e.state = EnrollmentStateRevoked
	e.result = nil
	e.logger.Info("identity revoked")
	return nil
}

// performEnrollment handles the actual enrollment protocol.
func (e *Enrollor) performEnrollment(token *EnrollmentToken) (*EnrollmentResult, error) {
	controller := token.Controller
	if controller == "" {
		controller = e.config.Controller
	}

	if controller == "" {
		return nil, fmt.Errorf("no controller URL specified")
	}

	// TODO: Implement actual OpenZiti enrollment protocol:
	// 1. Generate CSR (Certificate Signing Request)
	// 2. Submit CSR to controller with enrollment token
	// 3. Receive signed certificate and CA bundle
	// 4. Store credentials securely

	result := &EnrollmentResult{
		IdentityID: token.Token[:8], // Placeholder
		CertPath:   filepath.Join(e.config.IdentityDir, "client.crt"),
		KeyPath:    filepath.Join(e.config.IdentityDir, "client.key"),
		CAPath:     filepath.Join(e.config.IdentityDir, "ca.crt"),
		ConfigPath: filepath.Join(e.config.IdentityDir, "config.json"),
		EnrolledAt: time.Now(),
		Controller: controller,
	}

	e.logger.Info("enrollment protocol completed",
		"controller", controller,
		"method", token.Method,
	)

	return result, nil
}

// renewalLoop monitors certificate expiry and triggers renewal.
func (e *Enrollor) renewalLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			expiry, err := e.CertificateExpiry()
			if err != nil {
				e.logger.Warn("failed to check certificate expiry", "error", err)
				continue
			}

			timeToExpiry := time.Until(expiry)
			if timeToExpiry <= e.config.RenewalThreshold {
				e.logger.Info("certificate nearing expiry, attempting renewal",
					"expires_in", timeToExpiry,
					"threshold", e.config.RenewalThreshold,
				)
				// TODO: Implement certificate renewal
			}
		}
	}
}
