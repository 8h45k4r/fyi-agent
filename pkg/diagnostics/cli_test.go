// Copyright 2024 FYI Agent Authors. Licensed under Apache-2.0.

package diagnostics

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if cfg.MetricsPort != 9090 {
		t.Errorf("expected MetricsPort 9090, got %d", cfg.MetricsPort)
	}
	if cfg.HealthPort != 8080 {
		t.Errorf("expected HealthPort 8080, got %d", cfg.HealthPort)
	}
	if cfg.MetricsPath != "/metrics" {
		t.Errorf("expected MetricsPath /metrics, got %s", cfg.MetricsPath)
	}
	if cfg.HealthPath != "/healthz" {
		t.Errorf("expected HealthPath /healthz, got %s", cfg.HealthPath)
	}
}

func TestStatusConstants(t *testing.T) {
	if StatusHealthy != "healthy" {
		t.Errorf("StatusHealthy = %q", StatusHealthy)
	}
	if StatusDegraded != "degraded" {
		t.Errorf("StatusDegraded = %q", StatusDegraded)
	}
	if StatusUnhealthy != "unhealthy" {
		t.Errorf("StatusUnhealthy = %q", StatusUnhealthy)
	}
}

func TestGetSystemInfo(t *testing.T) {
	info := GetSystemInfo()
	if info.OS != runtime.GOOS {
		t.Errorf("expected OS %s, got %s", runtime.GOOS, info.OS)
	}
	if info.Arch != runtime.GOARCH {
		t.Errorf("expected Arch %s, got %s", runtime.GOARCH, info.Arch)
	}
	if info.GoVersion == "" {
		t.Error("expected non-empty GoVersion")
	}
	if info.NumCPU <= 0 {
		t.Errorf("expected NumCPU > 0, got %d", info.NumCPU)
	}
	if info.NumGoroutine <= 0 {
		t.Errorf("expected NumGoroutine > 0, got %d", info.NumGoroutine)
	}
}

func TestNewServer(t *testing.T) {
	cfg := DefaultConfig()
	s := NewServer(cfg, "1.0.0-test", nil)
	if s == nil {
		t.Fatal("expected non-nil server")
	}
	if s.version != "1.0.0-test" {
		t.Errorf("expected version 1.0.0-test, got %s", s.version)
	}
}

func TestNewServerWithLogger(t *testing.T) {
	logger := slog.Default()
	s := NewServer(DefaultConfig(), "1.0.0", logger)
	if s.logger != logger {
		t.Error("expected custom logger to be set")
	}
}

func TestHealthHandlerAllHealthy(t *testing.T) {
	s := NewServer(DefaultConfig(), "1.0.0", nil)
	s.RegisterCheck("tunnel", func() ComponentStatus {
		return ComponentStatus{Status: StatusHealthy, Message: "connected"}
	})
	s.RegisterCheck("dlp", func() ComponentStatus {
		return ComponentStatus{Status: StatusHealthy, Message: "active"}
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.HealthHandler()(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var report HealthReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if report.Status != StatusHealthy {
		t.Errorf("expected healthy, got %s", report.Status)
	}
	if len(report.Components) != 2 {
		t.Errorf("expected 2 components, got %d", len(report.Components))
	}
}

func TestHealthHandlerDegraded(t *testing.T) {
	s := NewServer(DefaultConfig(), "1.0.0", nil)
	s.RegisterCheck("tunnel", func() ComponentStatus {
		return ComponentStatus{Status: StatusHealthy}
	})
	s.RegisterCheck("ssl", func() ComponentStatus {
		return ComponentStatus{Status: StatusDegraded, Message: "cert expiring"}
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.HealthHandler()(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for degraded, got %d", w.Code)
	}

	var report HealthReport
	json.Unmarshal(w.Body.Bytes(), &report)
	if report.Status != StatusDegraded {
		t.Errorf("expected degraded, got %s", report.Status)
	}
}

func TestHealthHandlerUnhealthy(t *testing.T) {
	s := NewServer(DefaultConfig(), "1.0.0", nil)
	s.RegisterCheck("tunnel", func() ComponentStatus {
		return ComponentStatus{Status: StatusUnhealthy, Message: "disconnected"}
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.HealthHandler()(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for unhealthy, got %d", w.Code)
	}

	var report HealthReport
	json.Unmarshal(w.Body.Bytes(), &report)
	if report.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy, got %s", report.Status)
	}
}

func TestHealthHandlerNoChecks(t *testing.T) {
	s := NewServer(DefaultConfig(), "1.0.0", nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.HealthHandler()(w, req)

	var report HealthReport
	json.Unmarshal(w.Body.Bytes(), &report)
	if report.Status != StatusHealthy {
		t.Errorf("expected healthy with no checks, got %s", report.Status)
	}
	if report.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", report.Version)
	}
	if report.Uptime == "" {
		t.Error("expected non-empty uptime")
	}
}

func TestHealthReportJSON(t *testing.T) {
	s := NewServer(DefaultConfig(), "2.0.0", nil)
	s.RegisterCheck("test", func() ComponentStatus {
		return ComponentStatus{Status: StatusHealthy, Latency: "5ms"}
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.HealthHandler()(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected application/json, got %s", contentType)
	}
}

func TestTestConnectivityReachable(t *testing.T) {
	// Start a test TCP server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Extract host:port from the test server URL
	target := srv.Listener.Addr().String()
	result := TestConnectivity(context.Background(), target, 5*time.Second)

	if !result.Reachable {
		t.Errorf("expected target %s to be reachable", target)
	}
	if result.Error != "" {
		t.Errorf("expected no error, got %s", result.Error)
	}
	if result.Latency <= 0 {
		t.Error("expected positive latency")
	}
}

func TestTestConnectivityUnreachable(t *testing.T) {
	result := TestConnectivity(context.Background(), "192.0.2.1:1", 500*time.Millisecond)

	if result.Reachable {
		t.Error("expected unreachable for invalid target")
	}
	if result.Error == "" {
		t.Error("expected error message for unreachable target")
	}
}

func TestRegisterCheck(t *testing.T) {
	s := NewServer(DefaultConfig(), "1.0.0", nil)
	s.RegisterCheck("comp1", func() ComponentStatus {
		return ComponentStatus{Status: StatusHealthy}
	})
	s.RegisterCheck("comp2", func() ComponentStatus {
		return ComponentStatus{Status: StatusDegraded}
	})

	if len(s.checks) != 2 {
		t.Errorf("expected 2 checks, got %d", len(s.checks))
	}
}
