// Package tunnel manages the TUN network interface for the FYI Agent.
// It provides a platform-abstracted interface for creating and managing
// the virtual network device used for traffic interception.
package tunnel

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
)

// State represents the tunnel interface state.
type State int

const (
	StateClosed State = iota
	StateOpening
	StateOpen
	StatePaused
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpening:
		return "opening"
	case StateOpen:
		return "open"
	case StatePaused:
		return "paused"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// Config holds TUN interface configuration.
type Config struct {
	InterfaceName string   `yaml:"interface_name"`
	MTU           int      `yaml:"mtu"`
	DNSServers    []string `yaml:"dns"`
	Routes        []string `yaml:"routes"`
}

// DefaultConfig returns sensible TUN defaults.
func DefaultConfig() Config {
	return Config{
		InterfaceName: "fyi0",
		MTU:           1400,
		DNSServers:    []string{"10.0.0.1"},
		Routes:        []string{"10.0.0.0/8"},
	}
}

// Device represents a TUN device abstraction.
// Implementations are platform-specific (darwin, windows).
type Device interface {
	io.ReadWriteCloser
	// Name returns the interface name.
	Name() string
	// MTU returns the current MTU.
	MTU() int
}

// RouteEntry represents a routing table entry.
type RouteEntry struct {
	Destination *net.IPNet
	Gateway     net.IP
	Metric      int
}

// ParseRoutes parses CIDR strings into RouteEntry objects.
func ParseRoutes(cidrs []string) ([]RouteEntry, error) {
	routes := make([]RouteEntry, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("tunnel: invalid CIDR %q: %w", cidr, err)
		}
		routes = append(routes, RouteEntry{Destination: ipNet})
	}
	return routes, nil
}

// Manager manages the TUN device lifecycle.
type Manager struct {
	cfg    Config
	logger *slog.Logger
	device Device
	routes []RouteEntry
	state  State
	mu     sync.RWMutex
}

// NewManager creates a new tunnel manager.
func NewManager(cfg Config, logger *slog.Logger) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MTU < 576 {
		return nil, fmt.Errorf("tunnel: MTU %d is below minimum (576)", cfg.MTU)
	}
	if cfg.InterfaceName == "" {
		return nil, fmt.Errorf("tunnel: interface name cannot be empty")
	}

	routes, err := ParseRoutes(cfg.Routes)
	if err != nil {
		return nil, err
	}

	return &Manager{
		cfg:    cfg,
		logger: logger,
		routes: routes,
		state:  StateClosed,
	}, nil
}

// State returns the current tunnel state.
func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// Open creates and configures the TUN device.
// The actual device creation is platform-specific and injected via SetDevice.
func (m *Manager) Open(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == StateOpen {
		return fmt.Errorf("tunnel: already open")
	}

	m.state = StateOpening
	m.logger.Info("opening tunnel",
		"interface", m.cfg.InterfaceName,
		"mtu", m.cfg.MTU,
		"routes", len(m.routes),
	)

	// Device must be set via SetDevice before Open
	if m.device == nil {
		m.state = StateClosed
		return fmt.Errorf("tunnel: no device configured, call SetDevice first")
	}

	m.state = StateOpen
	m.logger.Info("tunnel opened", "interface", m.device.Name())
	return nil
}

// SetDevice injects a platform-specific TUN device.
func (m *Manager) SetDevice(dev Device) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.device = dev
}

// Close shuts down the TUN device.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == StateClosed {
		return nil
	}

	if m.device != nil {
		if err := m.device.Close(); err != nil {
			m.logger.Error("failed to close device", "error", err)
			return fmt.Errorf("tunnel: close device: %w", err)
		}
	}

	m.state = StateClosed
	m.logger.Info("tunnel closed")
	return nil
}

// Pause temporarily disables the tunnel (e.g., for captive portal).
func (m *Manager) Pause() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state != StateOpen {
		return fmt.Errorf("tunnel: cannot pause, state is %v", m.state)
	}

	m.state = StatePaused
	m.logger.Info("tunnel paused")
	return nil
}

// Resume re-enables a paused tunnel.
func (m *Manager) Resume() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state != StatePaused {
		return fmt.Errorf("tunnel: cannot resume, state is %v", m.state)
	}

	m.state = StateOpen
	m.logger.Info("tunnel resumed")
	return nil
}

// Routes returns the configured routes.
func (m *Manager) Routes() []RouteEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]RouteEntry, len(m.routes))
	copy(result, m.routes)
	return result
}
