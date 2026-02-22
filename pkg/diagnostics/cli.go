// Package diagnostics provides self-service troubleshooting tools
// for the FYI Agent, including health checks, connectivity tests,
// and log collection for support.
package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"time"
)

// Config holds diagnostics configuration.
type Config struct {
	Enabled       bool `yaml:"enabled"`
	MetricsPort   int  `yaml:"metrics_port"`
	HealthPort    int  `yaml:"health_port"`
	MetricsPath   string `yaml:"metrics_path"`
	HealthPath    string `yaml:"health_path"`
}

// DefaultConfig returns sensible diagnostics defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:     true,
		MetricsPort: 9090,
		HealthPort:  8080,
		MetricsPath: "/metrics",
		HealthPath:  "/healthz",
	}
}

// Status represents overall agent health status.
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

// HealthReport contains the agent health status.
type HealthReport struct {
	Status     Status                  `json:"status"`
	Timestamp  time.Time               `json:"timestamp"`
	Uptime     string                  `json:"uptime"`
	Version    string                  `json:"version"`
	Components map[string]ComponentStatus `json:"components"`
}

// ComponentStatus represents individual component health.
type ComponentStatus struct {
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
	Latency string `json:"latency,omitempty"`
}

// ConnectivityResult holds results of a connectivity test.
type ConnectivityResult struct {
	Target    string        `json:"target"`
	Reachable bool          `json:"reachable"`
	Latency   time.Duration `json:"latency"`
	Error     string        `json:"error,omitempty"`
}

// SystemInfo holds runtime system information.
type SystemInfo struct {
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	GoVersion    string `json:"go_version"`
	NumCPU       int    `json:"num_cpu"`
	NumGoroutine int    `json:"num_goroutine"`
	AllocMB      int64  `json:"alloc_mb"`
	TotalAllocMB int64  `json:"total_alloc_mb"`
}

// GetSystemInfo returns current system runtime info.
func GetSystemInfo() SystemInfo {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	return SystemInfo{
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		GoVersion:    runtime.Version(),
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		AllocMB:      int64(memStats.Alloc / 1024 / 1024),
		TotalAllocMB: int64(memStats.TotalAlloc / 1024 / 1024),
	}
}

// TestConnectivity tests connectivity to a target host:port.
func TestConnectivity(ctx context.Context, target string, timeout time.Duration) ConnectivityResult {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", target, timeout)
	latency := time.Since(start)

	result := ConnectivityResult{
		Target:  target,
		Latency: latency,
	}

	if err != nil {
		result.Reachable = false
		result.Error = err.Error()
	} else {
		result.Reachable = true
		_ = conn.Close()
	}
	return result
}

// Server serves health and metrics endpoints.
type Server struct {
	cfg       Config
	logger    *slog.Logger
	startTime time.Time
	version   string
	checks    map[string]func() ComponentStatus
}

// NewServer creates a new diagnostics server.
func NewServer(cfg Config, version string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		cfg:       cfg,
		logger:    logger,
		startTime: time.Now(),
		version:   version,
		checks:    make(map[string]func() ComponentStatus),
	}
}

// RegisterCheck adds a component health check.
func (s *Server) RegisterCheck(name string, check func() ComponentStatus) {
	s.checks[name] = check
}

// HealthHandler returns an HTTP handler for health checks.
func (s *Server) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report := s.buildHealthReport()
		w.Header().Set("Content-Type", "application/json")

		switch report.Status {
		case StatusHealthy:
			w.WriteHeader(http.StatusOK)
		case StatusDegraded:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		_ = json.NewEncoder(w).Encode(report)
	}
}

// Start starts the diagnostics HTTP server.
func (s *Server) Start(ctx context.Context) error {
	if !s.cfg.Enabled {
		s.logger.Info("diagnostics disabled")
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc(s.cfg.HealthPath, s.HealthHandler())
	mux.HandleFunc("/system", s.systemInfoHandler())

	addr := fmt.Sprintf(":%d", s.cfg.HealthPort)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	s.logger.Info("diagnostics server starting", "addr", addr)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("diagnostics: server error: %w", err)
	}
	return nil
}

func (s *Server) buildHealthReport() HealthReport {
	components := make(map[string]ComponentStatus)
	overall := StatusHealthy

	for name, check := range s.checks {
		status := check()
		components[name] = status
		if status.Status == StatusUnhealthy {
			overall = StatusUnhealthy
		} else if status.Status == StatusDegraded && overall != StatusUnhealthy {
			overall = StatusDegraded
		}
	}

	return HealthReport{
		Status:     overall,
		Timestamp:  time.Now(),
		Uptime:     time.Since(s.startTime).String(),
		Version:    s.version,
		Components: components,
	}
}

func (s *Server) systemInfoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GetSystemInfo())
	}
}
