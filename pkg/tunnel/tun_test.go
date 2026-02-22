package tunnel

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
)

// mockDevice implements the Device interface for testing.
type mockDevice struct {
	name      string
	mtu       int
	closed    bool
	closeErr  error
}

func (d *mockDevice) Read(p []byte) (int, error)  { return 0, nil }
func (d *mockDevice) Write(p []byte) (int, error) { return len(p), nil }
func (d *mockDevice) Close() error {
	d.closed = true
	return d.closeErr
}
func (d *mockDevice) Name() string { return d.name }
func (d *mockDevice) MTU() int     { return d.mtu }

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpening, "opening"},
		{StateOpen, "open"},
		{StatePaused, "paused"},
		{State(99), "unknown(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("State.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.InterfaceName != "fyi0" {
		t.Errorf("expected interface fyi0, got %q", cfg.InterfaceName)
	}
	if cfg.MTU != 1400 {
		t.Errorf("expected MTU 1400, got %d", cfg.MTU)
	}
	if len(cfg.DNSServers) != 1 || cfg.DNSServers[0] != "10.0.0.1" {
		t.Errorf("unexpected DNS servers: %v", cfg.DNSServers)
	}
	if len(cfg.Routes) != 1 || cfg.Routes[0] != "10.0.0.0/8" {
		t.Errorf("unexpected routes: %v", cfg.Routes)
	}
}

func TestParseRoutes(t *testing.T) {
	// Valid CIDRs
	routes, err := ParseRoutes([]string{"10.0.0.0/8", "192.168.0.0/16"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}
	if routes[0].Destination.String() != "10.0.0.0/8" {
		t.Errorf("route[0] = %v, want 10.0.0.0/8", routes[0].Destination)
	}

	// Invalid CIDR
	_, err = ParseRoutes([]string{"not-a-cidr"})
	if err == nil {
		t.Error("expected error for invalid CIDR")
	}

	// Empty list
	routes, err = ParseRoutes([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 0 {
		t.Errorf("expected 0 routes, got %d", len(routes))
	}
}

func TestNewManager(t *testing.T) {
	cfg := DefaultConfig()
	m, err := NewManager(cfg, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.State() != StateClosed {
		t.Errorf("expected StateClosed, got %v", m.State())
	}
}

func TestNewManager_NilLogger(t *testing.T) {
	cfg := DefaultConfig()
	m, err := NewManager(cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestNewManager_MTUTooLow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MTU = 100
	_, err := NewManager(cfg, slog.Default())
	if err == nil {
		t.Error("expected error for MTU below 576")
	}
}

func TestNewManager_EmptyInterfaceName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.InterfaceName = ""
	_, err := NewManager(cfg, slog.Default())
	if err == nil {
		t.Error("expected error for empty interface name")
	}
}

func TestNewManager_InvalidRoute(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Routes = []string{"invalid"}
	_, err := NewManager(cfg, slog.Default())
	if err == nil {
		t.Error("expected error for invalid route")
	}
}

func TestOpenWithoutDevice(t *testing.T) {
	cfg := DefaultConfig()
	m, _ := NewManager(cfg, slog.Default())
	err := m.Open(context.Background())
	if err == nil {
		t.Error("expected error when no device set")
	}
	if m.State() != StateClosed {
		t.Errorf("state should revert to closed, got %v", m.State())
	}
}

func TestOpenWithDevice(t *testing.T) {
	cfg := DefaultConfig()
	m, _ := NewManager(cfg, slog.Default())
	m.SetDevice(&mockDevice{name: "fyi0", mtu: 1400})

	err := m.Open(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.State() != StateOpen {
		t.Errorf("expected StateOpen, got %v", m.State())
	}
}

func TestOpenAlreadyOpen(t *testing.T) {
	cfg := DefaultConfig()
	m, _ := NewManager(cfg, slog.Default())
	m.SetDevice(&mockDevice{name: "fyi0", mtu: 1400})
	_ = m.Open(context.Background())

	err := m.Open(context.Background())
	if err == nil {
		t.Error("expected error for already open tunnel")
	}
}

func TestClose(t *testing.T) {
	cfg := DefaultConfig()
	m, _ := NewManager(cfg, slog.Default())
	dev := &mockDevice{name: "fyi0", mtu: 1400}
	m.SetDevice(dev)
	_ = m.Open(context.Background())

	err := m.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.State() != StateClosed {
		t.Errorf("expected StateClosed, got %v", m.State())
	}
	if !dev.closed {
		t.Error("expected device to be closed")
	}
}

func TestCloseAlreadyClosed(t *testing.T) {
	cfg := DefaultConfig()
	m, _ := NewManager(cfg, slog.Default())
	err := m.Close()
	if err != nil {
		t.Errorf("closing already-closed should return nil, got: %v", err)
	}
}

func TestCloseDeviceError(t *testing.T) {
	cfg := DefaultConfig()
	m, _ := NewManager(cfg, slog.Default())
	dev := &mockDevice{name: "fyi0", mtu: 1400, closeErr: fmt.Errorf("device error")}
	m.SetDevice(dev)
	_ = m.Open(context.Background())

	err := m.Close()
	if err == nil {
		t.Error("expected error when device close fails")
	}
}

func TestPauseResume(t *testing.T) {
	cfg := DefaultConfig()
	m, _ := NewManager(cfg, slog.Default())
	m.SetDevice(&mockDevice{name: "fyi0", mtu: 1400})
	_ = m.Open(context.Background())

	// Pause
	err := m.Pause()
	if err != nil {
		t.Fatalf("unexpected pause error: %v", err)
	}
	if m.State() != StatePaused {
		t.Errorf("expected StatePaused, got %v", m.State())
	}

	// Resume
	err = m.Resume()
	if err != nil {
		t.Fatalf("unexpected resume error: %v", err)
	}
	if m.State() != StateOpen {
		t.Errorf("expected StateOpen, got %v", m.State())
	}
}

func TestPauseWhenNotOpen(t *testing.T) {
	cfg := DefaultConfig()
	m, _ := NewManager(cfg, slog.Default())
	err := m.Pause()
	if err == nil {
		t.Error("expected error when pausing closed tunnel")
	}
}

func TestResumeWhenNotPaused(t *testing.T) {
	cfg := DefaultConfig()
	m, _ := NewManager(cfg, slog.Default())
	m.SetDevice(&mockDevice{name: "fyi0", mtu: 1400})
	_ = m.Open(context.Background())

	err := m.Resume()
	if err == nil {
		t.Error("expected error when resuming non-paused tunnel")
	}
}

func TestRoutes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Routes = []string{"10.0.0.0/8", "172.16.0.0/12"}
	m, _ := NewManager(cfg, slog.Default())

	routes := m.Routes()
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}
	// Verify routes are a copy (not same slice)
	routes[0].Metric = 999
	original := m.Routes()
	if original[0].Metric == 999 {
		t.Error("Routes() should return a copy, not a reference")
	}
}
