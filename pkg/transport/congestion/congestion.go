// Package congestion provides congestion control management for transport layer.
// It coordinates different congestion control algorithms (BBR, CUBIC) and
// provides adaptive selection based on network conditions.
package congestion

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Algorithm represents a congestion control algorithm type.
type Algorithm string

const (
	// AlgorithmBBR uses Bottleneck Bandwidth and Round-trip propagation time.
	AlgorithmBBR Algorithm = "bbr"
	// AlgorithmCUBIC uses the CUBIC TCP algorithm.
	AlgorithmCUBIC Algorithm = "cubic"
	// AlgorithmNone disables congestion control.
	AlgorithmNone Algorithm = "none"
)

// Stats holds current congestion control statistics.
type Stats struct {
	Algorithm    Algorithm     `json:"algorithm"`
	Bandwidth    uint64        `json:"bandwidth_bps"`
	RTT          time.Duration `json:"rtt"`
	PacketLoss   float64       `json:"packet_loss_pct"`
	Cwnd         uint64        `json:"cwnd_bytes"`
	Inflight     uint64        `json:"inflight_bytes"`
	Retransmits  uint64        `json:"retransmits"`
	LastUpdated  time.Time     `json:"last_updated"`
}

// Controller defines the interface for congestion control algorithms.
type Controller interface {
	// OnAck is called when an acknowledgment is received.
	OnAck(ackedBytes uint64, rtt time.Duration)
	// OnLoss is called when packet loss is detected.
	OnLoss(lostBytes uint64)
	// SendWindow returns the current congestion window in bytes.
	SendWindow() uint64
	// Stats returns current algorithm statistics.
	Stats() Stats
	// Reset resets the controller to initial state.
	Reset()
}

// ManagerConfig holds configuration for the congestion manager.
type ManagerConfig struct {
	// Algorithm specifies the congestion control algorithm to use.
	Algorithm Algorithm `yaml:"algorithm" json:"algorithm"`
	// InitialCwnd is the initial congestion window in bytes.
	InitialCwnd uint64 `yaml:"initial_cwnd" json:"initial_cwnd"`
	// MaxCwnd is the maximum congestion window in bytes.
	MaxCwnd uint64 `yaml:"max_cwnd" json:"max_cwnd"`
	// AdaptiveEnabled enables automatic algorithm switching.
	AdaptiveEnabled bool `yaml:"adaptive_enabled" json:"adaptive_enabled"`
	// ProbeInterval is how often to probe for better algorithms.
	ProbeInterval time.Duration `yaml:"probe_interval" json:"probe_interval"`
}

// DefaultManagerConfig returns sensible defaults for congestion management.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		Algorithm:       AlgorithmBBR,
		InitialCwnd:     10 * 1460, // 10 segments
		MaxCwnd:         10 * 1024 * 1024, // 10MB
		AdaptiveEnabled: true,
		ProbeInterval:   30 * time.Second,
	}
}

// Manager coordinates congestion control for multiple connections.
type Manager struct {
	mu          sync.RWMutex
	config      ManagerConfig
	controllers map[string]Controller
	logger      *slog.Logger
	stopCh      chan struct{}
	running     bool
}

// NewManager creates a new congestion control manager.
func NewManager(config ManagerConfig, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		config:      config,
		controllers: make(map[string]Controller),
		logger:      logger.With("component", "congestion-manager"),
		stopCh:      make(chan struct{}),
	}
}

// Start begins the congestion management loop.
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("congestion manager already running")
	}

	m.running = true
	m.logger.Info("congestion manager started",
		"algorithm", m.config.Algorithm,
		"adaptive", m.config.AdaptiveEnabled,
	)

	if m.config.AdaptiveEnabled {
		go m.adaptiveLoop()
	}

	return nil
}

// Stop halts the congestion management loop.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	close(m.stopCh)
	m.running = false
	m.logger.Info("congestion manager stopped")
}

// GetController returns or creates a controller for the given connection ID.
func (m *Manager) GetController(connID string) Controller {
	m.mu.RLock()
	if ctrl, ok := m.controllers[connID]; ok {
		m.mu.RUnlock()
		return ctrl
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock.
	if ctrl, ok := m.controllers[connID]; ok {
		return ctrl
	}

	ctrl := m.createController()
	m.controllers[connID] = ctrl
	m.logger.Debug("created congestion controller",
		"conn_id", connID,
		"algorithm", m.config.Algorithm,
	)
	return ctrl
}

// RemoveController removes the controller for a closed connection.
func (m *Manager) RemoveController(connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.controllers, connID)
}

// ActiveConnections returns the number of active connections.
func (m *Manager) ActiveConnections() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.controllers)
}

// AggregateStats returns combined statistics across all connections.
func (m *Manager) AggregateStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var agg Stats
	agg.Algorithm = m.config.Algorithm
	agg.LastUpdated = time.Now()

	for _, ctrl := range m.controllers {
		s := ctrl.Stats()
		agg.Bandwidth += s.Bandwidth
		agg.Cwnd += s.Cwnd
		agg.Inflight += s.Inflight
		agg.Retransmits += s.Retransmits
		if s.RTT > agg.RTT {
			agg.RTT = s.RTT
		}
		agg.PacketLoss += s.PacketLoss
	}

	if n := len(m.controllers); n > 0 {
		agg.PacketLoss /= float64(n)
	}

	return agg
}

// createController instantiates a congestion controller based on config.
func (m *Manager) createController() Controller {
	switch m.config.Algorithm {
	case AlgorithmBBR:
		return NewBBR(m.config.InitialCwnd, m.config.MaxCwnd)
	case AlgorithmCUBIC:
		return newCUBIC(m.config.InitialCwnd, m.config.MaxCwnd)
	default:
		return NewBBR(m.config.InitialCwnd, m.config.MaxCwnd)
	}
}

// adaptiveLoop periodically evaluates whether to switch algorithms.
func (m *Manager) adaptiveLoop() {
	ticker := time.NewTicker(m.config.ProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.evaluateAlgorithm()
		}
	}
}

// evaluateAlgorithm checks if switching algorithms would improve performance.
func (m *Manager) evaluateAlgorithm() {
	stats := m.AggregateStats()

	// High loss rate suggests switching away from BBR to CUBIC.
	if stats.PacketLoss > 5.0 && m.config.Algorithm == AlgorithmBBR {
		m.logger.Info("high packet loss detected, consider switching to CUBIC",
			"loss_pct", stats.PacketLoss,
		)
	}

	// Low loss + high bandwidth suggests BBR is optimal.
	if stats.PacketLoss < 1.0 && m.config.Algorithm == AlgorithmCUBIC {
		m.logger.Info("low packet loss detected, consider switching to BBR",
			"loss_pct", stats.PacketLoss,
		)
	}
}

// cubicController implements a basic CUBIC congestion control.
type cubicController struct {
	mu       sync.Mutex
	cwnd     uint64
	maxCwnd  uint64
	ssthresh uint64
	rtt      time.Duration
	lossRate float64
	retrans  uint64
}

func newCUBIC(initialCwnd, maxCwnd uint64) *cubicController {
	return &cubicController{
		cwnd:     initialCwnd,
		maxCwnd:  maxCwnd,
		ssthresh: maxCwnd / 2,
	}
}

func (c *cubicController) OnAck(ackedBytes uint64, rtt time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rtt = rtt

	if c.cwnd < c.ssthresh {
		// Slow start.
		c.cwnd += ackedBytes
	} else {
		// Congestion avoidance (simplified CUBIC growth).
		increment := ackedBytes * ackedBytes / c.cwnd
		if increment == 0 {
			increment = 1
		}
		c.cwnd += increment
	}

	if c.cwnd > c.maxCwnd {
		c.cwnd = c.maxCwnd
	}
}

func (c *cubicController) OnLoss(lostBytes uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.retrans++
	c.ssthresh = c.cwnd * 7 / 10 // Reduce to 70%
	c.cwnd = c.ssthresh
}

func (c *cubicController) SendWindow() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cwnd
}

func (c *cubicController) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{
		Algorithm:   AlgorithmCUBIC,
		Cwnd:        c.cwnd,
		RTT:         c.rtt,
		PacketLoss:  c.lossRate,
		Retransmits: c.retrans,
		LastUpdated: time.Now(),
	}
}

func (c *cubicController) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cwnd = 10 * 1460
	c.ssthresh = c.maxCwnd / 2
	c.retrans = 0
	c.lossRate = 0
}
