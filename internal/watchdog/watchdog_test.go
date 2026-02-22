package watchdog

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Enabled {
		t.Error("expected default config to be enabled")
	}
	if cfg.HealthCheckInterval != 15*time.Second {
		t.Errorf("expected 15s interval, got %v", cfg.HealthCheckInterval)
	}
	if cfg.MemoryLimitMB != 512 {
		t.Errorf("expected 512MB limit, got %d", cfg.MemoryLimitMB)
	}
	if cfg.MaxRestarts != 5 {
		t.Errorf("expected 5 max restarts, got %d", cfg.MaxRestarts)
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateIdle, "idle"},
		{StateRunning, "running"},
		{StateDegraded, "degraded"},
		{StateStopped, "stopped"},
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

func TestNew(t *testing.T) {
	cfg := DefaultConfig()
	w := New(cfg, nil)
	if w == nil {
		t.Fatal("expected non-nil watchdog")
	}
	if w.State() != StateIdle {
		t.Errorf("expected idle state, got %v", w.State())
	}
}

func TestRegisterCheck(t *testing.T) {
	w := New(DefaultConfig(), slog.Default())

	// Valid registration
	err := w.RegisterCheck("test", func(ctx context.Context) error { return nil })
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Duplicate registration
	err = w.RegisterCheck("test", func(ctx context.Context) error { return nil })
	if err == nil {
		t.Error("expected error for duplicate check")
	}

	// Empty name
	err = w.RegisterCheck("", func(ctx context.Context) error { return nil })
	if err == nil {
		t.Error("expected error for empty name")
	}

	// Nil function
	err = w.RegisterCheck("nil-check", nil)
	if err == nil {
		t.Error("expected error for nil check function")
	}
}

func TestWatchdogDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	w := New(cfg, slog.Default())

	ctx := context.Background()
	err := w.Start(ctx)
	if err != nil {
		t.Errorf("disabled watchdog should return nil, got: %v", err)
	}
}

func TestWatchdogStopViaChannel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HealthCheckInterval = 50 * time.Millisecond
	w := New(cfg, slog.Default())

	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		done <- w.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)
	w.Stop()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error on stop, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not stop in time")
	}

	if w.State() != StateStopped {
		t.Errorf("expected stopped state, got %v", w.State())
	}
}

func TestWatchdogStopViaContext(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HealthCheckInterval = 50 * time.Millisecond
	w := New(cfg, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- w.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not stop in time")
	}
}

func TestHealthCheckFailureTriggersDegraded(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HealthCheckInterval = 50 * time.Millisecond
	w := New(cfg, slog.Default())

	_ = w.RegisterCheck("failing", func(ctx context.Context) error {
		return fmt.Errorf("simulated failure")
	})

	restartCalled := false
	w.SetRestartFunc(func(ctx context.Context) error {
		restartCalled = true
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = w.Start(ctx) }()

	time.Sleep(200 * time.Millisecond)
	cancel()

	if !restartCalled {
		t.Error("expected restart function to be called on health check failure")
	}
}

func TestMaxRestartsEnforced(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxRestarts = 2
	cfg.RestartWindow = 1 * time.Minute
	w := New(cfg, slog.Default())

	restartCount := 0
	w.SetRestartFunc(func(ctx context.Context) error {
		restartCount++
		return nil
	})

	ctx := context.Background()
	// Trigger 5 restarts
	for i := 0; i < 5; i++ {
		w.triggerRestart(ctx, "test")
	}

	if restartCount != 2 {
		t.Errorf("expected 2 restarts (max), got %d", restartCount)
	}
}

func TestTriggerRestartWithNoFunc(t *testing.T) {
	cfg := DefaultConfig()
	w := New(cfg, slog.Default())

	// Should not panic when no restart func is set
	w.triggerRestart(context.Background(), "test")
}
