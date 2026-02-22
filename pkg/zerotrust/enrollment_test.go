package zerotrust

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnrollmentTokenExpiry(t *testing.T) {
	tests := []struct {
		name      string
		token     EnrollmentToken
		expected  bool
	}{
		{
			name: "not expired",
			token: EnrollmentToken{
				ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
			},
			expected: false,
		},
		{
			name: "expired",
			token: EnrollmentToken{
				ExpiresAt: time.Now().Add(-1 * time.Hour).Unix(),
			},
			expected: true,
		},
		{
			name: "no expiry set",
			token: EnrollmentToken{
				ExpiresAt: 0,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.IsExpired(); got != tt.expected {
				t.Errorf("IsExpired() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestEnrollmentStateString(t *testing.T) {
	tests := []struct {
		state    EnrollmentState
		expected string
	}{
		{EnrollmentStateUnenrolled, "unenrolled"},
		{EnrollmentStatePending, "pending"},
		{EnrollmentStateEnrolled, "enrolled"},
		{EnrollmentStateExpired, "expired"},
		{EnrollmentStateRevoked, "revoked"},
		{EnrollmentState(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}

func TestNewEnrollor(t *testing.T) {
	config := DefaultEnrollmentConfig()
	enrollor := NewEnrollor(config, nil)

	if enrollor == nil {
		t.Fatal("expected non-nil enrollor")
	}

	if enrollor.State() != EnrollmentStateUnenrolled {
		t.Errorf("expected unenrolled state, got %s", enrollor.State())
	}

	if enrollor.Result() != nil {
		t.Error("expected nil result before enrollment")
	}
}

func TestEnrollWithExpiredToken(t *testing.T) {
	config := DefaultEnrollmentConfig()
	config.IdentityDir = t.TempDir()
	enrollor := NewEnrollor(config, nil)

	token := EnrollmentToken{
		Token:      "test-token-12345",
		Controller: "https://controller.example.com",
		Method:     "ott",
		ExpiresAt:  time.Now().Add(-1 * time.Hour).Unix(),
	}

	tokenData, _ := json.Marshal(token)
	_, err := enrollor.Enroll(tokenData)
	if err == nil {
		t.Error("expected error for expired token")
	}

	if enrollor.State() != EnrollmentStateExpired {
		t.Errorf("expected expired state, got %s", enrollor.State())
	}
}

func TestEnrollWithValidToken(t *testing.T) {
	config := DefaultEnrollmentConfig()
	config.IdentityDir = t.TempDir()
	config.Controller = "https://ctrl.example.com"
	enrollor := NewEnrollor(config, nil)

	token := EnrollmentToken{
		Token:      "valid-token-67890",
		Controller: "https://ctrl.example.com",
		Method:     "ott",
		ExpiresAt:  time.Now().Add(1 * time.Hour).Unix(),
	}

	tokenData, _ := json.Marshal(token)
	result, err := enrollor.Enroll(tokenData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if enrollor.State() != EnrollmentStateEnrolled {
		t.Errorf("expected enrolled state, got %s", enrollor.State())
	}

	if result.Controller != "https://ctrl.example.com" {
		t.Errorf("unexpected controller: %s", result.Controller)
	}
}

func TestEnrollFromFile(t *testing.T) {
	config := DefaultEnrollmentConfig()
	config.IdentityDir = t.TempDir()
	enrollor := NewEnrollor(config, nil)

	token := EnrollmentToken{
		Token:      "file-token-11111",
		Controller: "https://ctrl.example.com",
		Method:     "ott",
		ExpiresAt:  time.Now().Add(1 * time.Hour).Unix(),
	}

	tokenData, _ := json.Marshal(token)
	tokenPath := filepath.Join(t.TempDir(), "token.jwt")
	if err := os.WriteFile(tokenPath, tokenData, 0600); err != nil {
		t.Fatalf("failed to write token file: %v", err)
	}

	result, err := enrollor.EnrollFromFile(tokenPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestEnrollFromFileMissing(t *testing.T) {
	config := DefaultEnrollmentConfig()
	enrollor := NewEnrollor(config, nil)

	_, err := enrollor.EnrollFromFile("/nonexistent/token.jwt")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestRevokeNotEnrolled(t *testing.T) {
	config := DefaultEnrollmentConfig()
	enrollor := NewEnrollor(config, nil)

	err := enrollor.Revoke()
	if err == nil {
		t.Error("expected error when revoking unenrolled identity")
	}
}

func TestRevokeEnrolled(t *testing.T) {
	config := DefaultEnrollmentConfig()
	config.IdentityDir = t.TempDir()
	enrollor := NewEnrollor(config, nil)

	// Enroll first.
	token := EnrollmentToken{
		Token:      "revoke-test-token",
		Controller: "https://ctrl.example.com",
		Method:     "ott",
		ExpiresAt:  time.Now().Add(1 * time.Hour).Unix(),
	}
	tokenData, _ := json.Marshal(token)
	_, err := enrollor.Enroll(tokenData)
	if err != nil {
		t.Fatalf("enrollment failed: %v", err)
	}

	// Revoke.
	err = enrollor.Revoke()
	if err != nil {
		t.Fatalf("revoke failed: %v", err)
	}

	if enrollor.State() != EnrollmentStateRevoked {
		t.Errorf("expected revoked state, got %s", enrollor.State())
	}

	if enrollor.Result() != nil {
		t.Error("expected nil result after revocation")
	}
}

func TestAutoRenewalDisabled(t *testing.T) {
	config := DefaultEnrollmentConfig()
	config.AutoRenew = false
	enrollor := NewEnrollor(config, nil)

	err := enrollor.StartAutoRenewal()
	if err == nil {
		t.Error("expected error when auto-renewal is disabled")
	}
}

func TestDefaultEnrollmentConfig(t *testing.T) {
	config := DefaultEnrollmentConfig()

	if config.IdentityDir == "" {
		t.Error("expected non-empty identity dir")
	}

	if !config.AutoRenew {
		t.Error("expected auto-renew to be enabled by default")
	}

	if config.RenewalThreshold != 72*time.Hour {
		t.Errorf("expected 72h renewal threshold, got %v", config.RenewalThreshold)
	}
}
