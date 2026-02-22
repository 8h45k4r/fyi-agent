// Copyright 2024 FYI Agent Authors. Licensed under Apache-2.0.

// Package tray provides a system tray icon with status menu for the FYI Agent.
// It shows connection state, policy status, and quick actions on both
// Windows (notification area) and macOS (menu bar).
package tray

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// State represents the current agent connection state shown in the tray icon.
type State int

const (
	StateDisconnected State = iota // Grey icon - not connected
	StateConnecting                // Yellow icon - establishing tunnel
	StateConnected                 // Green icon - fully operational
	StateError                     // Red icon - error condition
)

// String returns a human-readable state label.
func (s State) String() string {
	switch s {
	case StateDisconnected:
		return "Disconnected"
	case StateConnecting:
		return "Connecting..."
	case StateConnected:
		return "Connected"
	case StateError:
		return "Error"
	default:
		return fmt.Sprintf("Unknown(%d)", int(s))
	}
}

// MenuItem represents a single menu entry in the tray context menu.
type MenuItem struct {
	Label    string
	Enabled  bool
	Action   func()
	Children []MenuItem
}

// StatusInfo holds the data displayed in the tray tooltip and menu.
type StatusInfo struct {
	State      State
	Version    string
	Identity   string // Current zero-trust identity
	PolicyName string // Active policy name
	Uptime     string
	DLPBlocked int    // Number of DLP events blocked in current session
}

// EventHandler is called when the user interacts with the tray.
type EventHandler interface {
	OnConnect()
	OnDisconnect()
	OnShowLogs()
	OnShowDiagnostics()
	OnQuit()
}

// Tray manages the system tray icon lifecycle.
type Tray struct {
	mu      sync.RWMutex
	logger  *slog.Logger
	status  StatusInfo
	handler EventHandler
	done    chan struct{}
	started bool
}

// Config holds tray configuration options.
type Config struct {
	Version  string
	Logger   *slog.Logger
	Handler  EventHandler
}

// New creates a new system tray manager.
func New(cfg Config) *Tray {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Tray{
		logger:  logger,
		handler: cfg.Handler,
		status: StatusInfo{
			State:   StateDisconnected,
			Version: cfg.Version,
		},
		done: make(chan struct{}),
	}
}

// SetState updates the tray icon and tooltip to reflect the new state.
func (t *Tray) SetState(state State) {
	t.mu.Lock()
	t.status.State = state
	t.mu.Unlock()
	t.logger.Info("tray state changed", "state", state.String())
}

// SetIdentity updates the displayed identity information.
func (t *Tray) SetIdentity(identity string) {
	t.mu.Lock()
	t.status.Identity = identity
	t.mu.Unlock()
}

// SetPolicy updates the displayed active policy name.
func (t *Tray) SetPolicy(name string) {
	t.mu.Lock()
	t.status.PolicyName = name
	t.mu.Unlock()
}

// SetUptime updates the displayed uptime string.
func (t *Tray) SetUptime(uptime string) {
	t.mu.Lock()
	t.status.Uptime = uptime
	t.mu.Unlock()
}

// IncrementDLPBlocked adds to the DLP blocked counter.
func (t *Tray) IncrementDLPBlocked() {
	t.mu.Lock()
	t.status.DLPBlocked++
	t.mu.Unlock()
}

// Status returns a snapshot of the current tray status.
func (t *Tray) Status() StatusInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.status
}

// BuildMenu constructs the context menu items based on current state.
func (t *Tray) BuildMenu() []MenuItem {
	status := t.Status()
	items := []MenuItem{
		{Label: fmt.Sprintf("FYI Agent %s", status.Version), Enabled: false},
		{Label: fmt.Sprintf("Status: %s", status.State.String()), Enabled: false},
	}

	if status.Identity != "" {
		items = append(items, MenuItem{
			Label: fmt.Sprintf("Identity: %s", status.Identity), Enabled: false,
		})
	}
	if status.PolicyName != "" {
		items = append(items, MenuItem{
			Label: fmt.Sprintf("Policy: %s", status.PolicyName), Enabled: false,
		})
	}
	if status.Uptime != "" {
		items = append(items, MenuItem{
			Label: fmt.Sprintf("Uptime: %s", status.Uptime), Enabled: false,
		})
	}
	if status.DLPBlocked > 0 {
		items = append(items, MenuItem{
			Label: fmt.Sprintf("DLP Blocked: %d", status.DLPBlocked), Enabled: false,
		})
	}

	// Action items
	switch status.State {
	case StateDisconnected, StateError:
		items = append(items, MenuItem{Label: "Connect", Enabled: true, Action: t.handler.OnConnect})
	case StateConnected, StateConnecting:
		items = append(items, MenuItem{Label: "Disconnect", Enabled: true, Action: t.handler.OnDisconnect})
	}

	items = append(items,
		MenuItem{Label: "View Logs", Enabled: true, Action: t.handler.OnShowLogs},
		MenuItem{Label: "Diagnostics", Enabled: true, Action: t.handler.OnShowDiagnostics},
		MenuItem{Label: "Quit", Enabled: true, Action: t.handler.OnQuit},
	)

	return items
}

// Start initialises the system tray. This is a placeholder for the actual
// native tray integration (e.g., using getlantern/systray or similar).
func (t *Tray) Start(ctx context.Context) error {
	t.mu.Lock()
	if t.started {
		t.mu.Unlock()
		return fmt.Errorf("tray: already started")
	}
	t.started = true
	t.mu.Unlock()

	t.logger.Info("system tray started", "version", t.status.Version)

	// Block until context is cancelled or Stop is called
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.done:
		return nil
	}
}

// Stop shuts down the system tray.
func (t *Tray) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		close(t.done)
		t.started = false
		t.logger.Info("system tray stopped")
	}
}
