package ssl

import (
	"crypto/tls"
	"log/slog"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Enabled {
		t.Error("default config should be enabled")
	}
	if cfg.MinTLSVersion != "1.2" {
		t.Errorf("expected min TLS 1.2, got %s", cfg.MinTLSVersion)
	}
	if len(cfg.BypassDomains) != 1 || cfg.BypassDomains[0] != "localhost" {
		t.Error("expected default bypass domain of localhost")
	}
}

func TestNewInspector(t *testing.T) {
	cfg := DefaultConfig()
	i, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if i == nil {
		t.Fatal("New returned nil inspector")
	}
}

func TestNewInspectorNilLogger(t *testing.T) {
	cfg := DefaultConfig()
	i, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New with nil logger returned error: %v", err)
	}
	if i == nil {
		t.Fatal("New returned nil inspector")
	}
}

func TestNewInspectorInvalidTLSVersion(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinTLSVersion = "0.9"
	_, err := New(cfg, nil)
	if err == nil {
		t.Error("expected error for invalid TLS version")
	}
}

func TestShouldInspect(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BypassDomains = []string{"localhost", "*.internal.corp"}
	i, _ := New(cfg, nil)

	tests := []struct {
		domain string
		want   bool
	}{
		{"example.com", true},
		{"localhost", false},
		{"app.internal.corp", false},
		{"internal.corp", false},
		{"google.com", true},
	}

	for _, tt := range tests {
		got := i.ShouldInspect(tt.domain)
		if got != tt.want {
			t.Errorf("ShouldInspect(%q) = %v, want %v", tt.domain, got, tt.want)
		}
	}
}

func TestShouldInspectDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	i, _ := New(cfg, nil)

	if i.ShouldInspect("example.com") {
		t.Error("disabled inspector should not inspect any domain")
	}
}

func TestMinTLSVersion(t *testing.T) {
	tests := []struct {
		version string
		want    uint16
	}{
		{"1.0", tls.VersionTLS10},
		{"1.1", tls.VersionTLS11},
		{"1.2", tls.VersionTLS12},
		{"1.3", tls.VersionTLS13},
	}

	for _, tt := range tests {
		cfg := DefaultConfig()
		cfg.MinTLSVersion = tt.version
		i, err := New(cfg, nil)
		if err != nil {
			t.Fatalf("New with TLS %s returned error: %v", tt.version, err)
		}
		if i.MinTLSVersion() != tt.want {
			t.Errorf("MinTLSVersion for %s: got %d, want %d", tt.version, i.MinTLSVersion(), tt.want)
		}
	}
}

func TestAddBypassDomain(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BypassDomains = []string{}
	i, _ := New(cfg, nil)

	if !i.ShouldInspect("bypass.example.com") {
		t.Error("should inspect before adding bypass")
	}

	i.AddBypassDomain("bypass.example.com")

	if i.ShouldInspect("bypass.example.com") {
		t.Error("should not inspect after adding bypass")
	}
}

func TestAddBypassWildcard(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BypassDomains = []string{}
	i, _ := New(cfg, nil)

	i.AddBypassDomain("*.test.com")

	if i.ShouldInspect("app.test.com") {
		t.Error("should not inspect wildcard match")
	}
	if !i.ShouldInspect("other.com") {
		t.Error("should inspect non-matching domain")
	}
}

func TestRemoveBypassDomain(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BypassDomains = []string{"skip.example.com"}
	i, _ := New(cfg, nil)

	if i.ShouldInspect("skip.example.com") {
		t.Error("should not inspect bypassed domain")
	}

	i.RemoveBypassDomain("skip.example.com")

	if !i.ShouldInspect("skip.example.com") {
		t.Error("should inspect after removing bypass")
	}
}

func TestCaseInsensitivity(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BypassDomains = []string{"Example.COM"}
	i, _ := New(cfg, nil)

	if i.ShouldInspect("example.com") {
		t.Error("domain matching should be case-insensitive")
	}
}
