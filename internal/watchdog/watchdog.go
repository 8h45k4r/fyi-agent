// Package watchdog monitors the FYI Agent process health and performs
// automatic recovery when resource limits are exceeded or crashes occur.
package watchdog

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// State represents the current watchdog state.
type State int

const (
	StateIdle State = iota
	StateRunning
	StateDegraded
	StateStopped
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateRunning:
		return "running"
	case StateDegraded:
		return "degraded"
	case StateStopped:
		return "stopped"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// Config holds watchdog configuration.
type Config struct {
	Enabled              bool          `yaml:"enabled"`
	HealthCheckInterval  time.Duration `yaml:"health_check_interval"`
	MemoryLimitMB        int64         `yaml:"memory_limit_mb"`
	CPUThresholdPercent  float64       `yaml:"cpu_threshold_percent"`
	MaxRestarts          int           `yaml:"max_restarts"`
	RestartWindow        time.Duration `yaml:"restart_window"`
}

// DefaultConfig returns sensible defaults for watchdog configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:             true,
		HealthCheckInterval: 15 * time.Second,
		MemoryLimitMB:       512,
		CPUThresholdPercent: 80.0,
		MaxRestarts:         5,
		RestartWindow:       5 * time.Minute,
	}
}

// HealthCheck is a function that returns nil if the subsystem is healthy.
type HealthCheck func(ctx context.Context) error

// RestartFunc is called when the watchdog determines a restart is needed.
type RestartFunc func(ctx context.Context) error

// Watchdog monitors process health and triggers recovery.
type Watchdog struct {
	cfg           Config
	logger        *slog.Logger
	state         atomic.Int32
	restartCount  int
	restartTimes  []time.Time
	checks        map[string]HealthCheck
	restartFn     RestartFunc
	mu            sync.RWMutex
	stopCh        chan struct{}
}

// New creates a new Watchdog with the given configuration.
func New(cfg Config, logger *slog.Logger) *Watchdog {
	if logger == nil {
		logger = slog.Default()
	}
	w := &Watchdog{
		cfg:    cfg,
		logger: logger,
		checks: make(map[string]HealthCheck),
		stopCh: make(chan struct{}),
	}
	w.state.Store(int32(StateIdle))
	return w
}

// RegisterCheck adds a named health check to the watchdog.
func (w *Watchdog) RegisterCheck(name string, check HealthCheck) error {
	if name == "" {
		return fmt.Errorf("watchdog: check name cannot be empty")
	}
	if check == nil {
		return fmt.Errorf("watchdog: check function cannot be nil")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.checks[name]; exists {
		return fmt.Errorf("watchdog: check %q already registered", name)
	}
	w.checks[name] = check
	w.logger.Info("registered health check", "name", name)
	return nil
}

// SetRestartFunc sets the function to call when restart is needed.
func (w *Watchdog) SetRestartFunc(fn RestartFunc) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.restartFn = fn
}

// State returns the current watchdog state.
func (w *Watchdog) State() State {
	return State(w.state.Load())
}

// Start begins the watchdog monitoring loop.
func (w *Watchdog) Start(ctx context.Context) error {
	if !w.cfg.Enabled {
		w.logger.Info("watchdog disabled, skipping")
		return nil
	}
	w.state.Store(int32(StateRunning))
	w.logger.Info("watchdog started",
		"interval", w.cfg.HealthCheckInterval,
		"memory_limit_mb", w.cfg.MemoryLimitMB,
	)

	ticker := time.NewTicker(w.cfg.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.state.Store(int32(StateStopped))
			w.logger.Info("watchdog stopped via context")
			return ctx.Err()
		case <-w.stopCh:
			w.state.Store(int32(StateStopped))
			w.logger.Info("watchdog stopped")
			return nil
		case <-ticker.C:
			w.performChecks(ctx)
		}
	}
}

// Stop signals the watchdog to stop.
func (w *Watchdog) Stop() {
	select {
	case w.stopCh <- struct{}{}:
	default:
	}
}

// performChecks runs all health checks and resource monitoring.
func (w *Watchdog) performChecks(ctx context.Context) {
	// Check memory usage
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	allocMB := int64(memStats.Alloc / 1024 / 1024)

	if w.cfg.MemoryLimitMB > 0 && allocMB > w.cfg.MemoryLimitMB {
		w.logger.Warn("memory limit exceeded",
			"current_mb", allocMB,
			"limit_mb", w.cfg.MemoryLimitMB,
		)
		w.state.Store(int32(StateDegraded))
		w.triggerRestart(ctx, fmt.Sprintf("memory limit exceeded: %dMB > %dMB", allocMB, w.cfg.MemoryLimitMB))
		return
	}

	// Run registered health checks
	w.mu.RLock()
	checks := make(map[string]HealthCheck, len(w.checks))
	for k, v := range w.checks {
		checks[k] = v
	}
	w.mu.RUnlock()

	var failures []string
	for name, check := range checks {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := check(checkCtx); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", name, err))
			w.logger.Warn("health check failed", "name", name, "error", err)
		}
		cancel()
	}

	if len(failures) > 0 {
		w.state.Store(int32(StateDegraded))
		w.triggerRestart(ctx, fmt.Sprintf("%d health check(s) failed", len(failures)))
	} else {
		w.state.Store(int32(StateRunning))
	}
}

// triggerRestart attempts to restart the agent if within restart limits.
func (w *Watchdog) triggerRestart(ctx context.Context, reason string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	// Prune restart times outside the window
	var recent []time.Time
	for _, t := range w.restartTimes {
		if now.Sub(t) < w.cfg.RestartWindow {
			recent = append(recent, t)
		}
	}
	w.restartTimes = recent

	if len(w.restartTimes) >= w.cfg.MaxRestarts {
		w.logger.Error("max restarts exceeded within window, giving up",
			"max_restarts", w.cfg.MaxRestarts,
			"window", w.cfg.RestartWindow,
			"reason", reason,
		)
		return
	}

	if w.restartFn == nil {
		w.logger.Warn("restart needed but no restart function registered", "reason", reason)
		return
	}

	w.logger.Info("triggering restart", "reason", reason, "attempt", len(w.restartTimes)+1)
	w.restartTimes = append(w.restartTimes, now)

	if err := w.restartFn(ctx); err != nil {
		w.logger.Error("restart failed", "error", err)
	}
}
