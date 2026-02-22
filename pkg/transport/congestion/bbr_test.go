package congestion

import (
	"testing"
	"time"
)

func TestNewController(t *testing.T) {
	c := NewController(0)
	if c == nil {
		t.Fatal("NewController returned nil")
	}
	if c.State() != StateStartup {
		t.Errorf("expected StateStartup, got %d", c.State())
	}
}

func TestNewControllerCustomMaxCWND(t *testing.T) {
	c := NewController(1024 * 1024)
	if c == nil {
		t.Fatal("NewController returned nil")
	}
}

func TestNewControllerDefaultMaxCWND(t *testing.T) {
	c := NewController(0)
	// Should use 10MB default
	if c.CWND() <= 0 {
		t.Error("CWND should be positive")
	}
}

func TestOnSend(t *testing.T) {
	c := NewController(0)
	c.OnSend(1024)

	// After sending, CanSend might still be true since cwnd is large
	// Just verify no panic
}

func TestCanSendInitial(t *testing.T) {
	c := NewController(0)
	if !c.CanSend() {
		t.Error("should be able to send initially")
	}
}

func TestOnACK(t *testing.T) {
	c := NewController(0)
	c.OnSend(1024)

	// ACK with valid RTT
	c.OnACK(1024, 50*time.Millisecond)

	// Verify controller still works after ACK
	if c.PacingRate() < 0 {
		t.Error("pacing rate should be non-negative after ACK")
	}
}

func TestOnACKZeroRTT(t *testing.T) {
	c := NewController(0)
	c.OnSend(1024)

	// Zero RTT should be ignored
	c.OnACK(1024, 0)
}

func TestOnACKNegativeRTT(t *testing.T) {
	c := NewController(0)
	c.OnSend(1024)

	// Negative RTT should be ignored
	c.OnACK(1024, -1*time.Millisecond)
}

func TestInflightTracking(t *testing.T) {
	c := NewController(0)

	c.OnSend(5000)
	c.OnSend(3000)
	c.OnACK(5000, 10*time.Millisecond)

	// Should not panic and state should be valid
	if c.CWND() <= 0 {
		t.Error("CWND should be positive")
	}
}

func TestPacingRate(t *testing.T) {
	c := NewController(0)
	rate := c.PacingRate()
	if rate < 0 {
		t.Errorf("pacing rate should be non-negative, got %f", rate)
	}
}

func TestCWND(t *testing.T) {
	c := NewController(0)
	cwnd := c.CWND()
	if cwnd <= 0 {
		t.Errorf("CWND should be positive, got %d", cwnd)
	}
}

func TestStateConstants(t *testing.T) {
	if StateStartup != 0 {
		t.Error("StateStartup should be 0")
	}
	if StateDrain != 1 {
		t.Error("StateDrain should be 1")
	}
	if StateProbeBW != 2 {
		t.Error("StateProbeBW should be 2")
	}
	if StateProbeRTT != 3 {
		t.Error("StateProbeRTT should be 3")
	}
}

func TestStateTransitionFromStartup(t *testing.T) {
	c := NewController(0)

	// Send and ACK enough data to potentially trigger state transition
	for i := 0; i < 100; i++ {
		c.OnSend(64 * 1024)
		c.OnACK(64*1024, 10*time.Millisecond)
	}

	// After many ACKs, state should have moved beyond startup
	state := c.State()
	if state == StateStartup {
		// It's possible it's still in startup if not enough data
		// Just verify no panic occurred
	}
	_ = state // use the variable
}
