// Package congestion implements BBR-style congestion control for the FYI Agent transport layer.
// This fixes OpenZiti issue #2577 where throughput collapses with 2-8 concurrent connections.
package congestion

import (
	"math"
	"sync"
	"time"
)

// State represents the BBR state machine phases.
type State int

const (
	StateStartup    State = iota // Exponential growth to find bandwidth
	StateDrain                   // Drain excess queue from startup
	StateProbeBW                 // Steady-state bandwidth probing
	StateProbeRTT                // Periodic RTT measurement
)

// Controller implements BBR congestion control adapted for the Ziti channel protocol.
type Controller struct {
	mu sync.Mutex

	state       State
	bandwidth   float64       // Estimated bottleneck bandwidth (bytes/sec)
	rtProp      time.Duration // Minimum observed RTT
	pacingRate  float64       // Current pacing rate (bytes/sec)
	cwnd        int           // Congestion window (bytes)
	inflight    int           // Current bytes in flight
	delivered   int           // Total bytes delivered
	deliveredAt time.Time     // Time of last delivery measurement

	// Windowed min/max filters
	minRTT      time.Duration
	minRTTStamp time.Time
	maxBW       float64
	maxBWStamp  time.Time

	// Configuration
	maxCWND    int
	initialRTT time.Duration
}

// NewController creates a new BBR congestion controller.
func NewController(maxCWND int) *Controller {
	if maxCWND <= 0 {
		maxCWND = 10 * 1024 * 1024 // 10MB default
	}

	now := time.Now()
	return &Controller{
		state:       StateStartup,
		rtProp:      100 * time.Millisecond,
		cwnd:        64 * 1024, // 64KB initial window
		maxCWND:     maxCWND,
		initialRTT:  100 * time.Millisecond,
		minRTT:      time.Duration(math.MaxInt64),
		minRTTStamp: now,
		maxBWStamp:  now,
		deliveredAt: now,
	}
}

// OnACK processes an acknowledgment and updates BBR state.
func (c *Controller) OnACK(bytesACKed int, rtt time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if rtt <= 0 {
		return
	}

	now := time.Now()
	c.delivered += bytesACKed
	c.inflight -= bytesACKed
	if c.inflight < 0 {
		c.inflight = 0
	}

	// Update minimum RTT (windowed, 10 second window)
	if rtt < c.minRTT || now.Sub(c.minRTTStamp) > 10*time.Second {
		c.minRTT = rtt
		c.minRTTStamp = now
	}

	// Calculate delivery rate
	elapsed := now.Sub(c.deliveredAt)
	if elapsed > 0 {
		deliveryRate := float64(bytesACKed) / elapsed.Seconds()
		if deliveryRate > c.maxBW || now.Sub(c.maxBWStamp) > 10*time.Second {
			c.maxBW = deliveryRate
			c.maxBWStamp = now
		}
	}
	c.deliveredAt = now

	c.updateState()
	c.updatePacingRate()
	c.updateCWND()
}

// OnSend records bytes being sent.
func (c *Controller) OnSend(bytesSent int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inflight += bytesSent
}

// CanSend returns true if sending is allowed within the congestion window.
func (c *Controller) CanSend() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inflight < c.cwnd
}

// PacingRate returns the current pacing rate in bytes/sec.
func (c *Controller) PacingRate() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pacingRate
}

// CWND returns the current congestion window size.
func (c *Controller) CWND() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cwnd
}

// State returns the current BBR state.
func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Controller) updateState() {
	switch c.state {
	case StateStartup:
		if c.maxBW > 0 && c.delivered > c.cwnd*3 {
			c.state = StateDrain
		}
	case StateDrain:
		if c.inflight <= int(c.maxBW*c.minRTT.Seconds()) {
			c.state = StateProbeBW
		}
	case StateProbeBW:
		if time.Since(c.minRTTStamp) > 10*time.Second {
			c.state = StateProbeRTT
		}
	case StateProbeRTT:
		if time.Since(c.minRTTStamp) < 200*time.Millisecond {
			c.state = StateProbeBW
		}
	}
}

func (c *Controller) updatePacingRate() {
	var gain float64
	switch c.state {
	case StateStartup:
		gain = 2.885 // 2/ln(2)
	case StateDrain:
		gain = 1.0 / 2.885
	case StateProbeBW:
		gain = 1.0
	case StateProbeRTT:
		gain = 1.0
	}
	c.pacingRate = gain * c.maxBW
}

func (c *Controller) updateCWND() {
	target := int(c.maxBW * c.minRTT.Seconds())
	if target < 64*1024 {
		target = 64 * 1024
	}
	if target > c.maxCWND {
		target = c.maxCWND
	}

	switch c.state {
	case StateStartup:
		c.cwnd = target * 3
	case StateDrain:
		if c.cwnd > target {
			c.cwnd = target
		}
	case StateProbeBW:
		c.cwnd = target * 2
	case StateProbeRTT:
		c.cwnd = 4 * 1460 // 4 segments minimum
	}

	if c.cwnd > c.maxCWND {
		c.cwnd = c.maxCWND
	}
}
