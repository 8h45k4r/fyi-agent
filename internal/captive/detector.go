// Package captive provides captive portal detection for the FYI Agent.
package captive

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// State represents the captive portal detection state.
type State int

const (
	StateUnknown  State = iota
	StateOpen                   // No captive portal, internet is open
	StateCaptive                // Captive portal detected
	StateNoNetwork              // No network connectivity
)

// Detector probes for captive portals on network changes.
type Detector struct {
	mu       sync.RWMutex
	state    State
	probeURL string
	client   *http.Client
	timeout  time.Duration
}

// NewDetector creates a new captive portal detector.
func NewDetector(probeURL string, timeout time.Duration) *Detector {
	if probeURL == "" {
		probeURL = "http://captive.certifyi.ai/generate_204"
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	return &Detector{
		state:    StateUnknown,
		probeURL: probeURL,
		timeout:  timeout,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // Don't follow redirects
			},
		},
	}
}

// Probe checks for a captive portal and returns the current state.
func (d *Detector) Probe(ctx context.Context) (State, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.probeURL, nil)
	if err != nil {
		return StateNoNetwork, fmt.Errorf("creating probe request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		d.setState(StateNoNetwork)
		return StateNoNetwork, fmt.Errorf("probe request failed: %w", err)
	}
	defer resp.Body.Close()

	var newState State
	switch {
	case resp.StatusCode == http.StatusNoContent:
		newState = StateOpen
	case resp.StatusCode == http.StatusOK:
		newState = StateOpen
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		newState = StateCaptive
	default:
		newState = StateCaptive
	}

	d.setState(newState)
	return newState, nil
}

// State returns the last known captive portal state.
func (d *Detector) State() State {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.state
}

// ProbeURL returns the configured probe URL.
func (d *Detector) ProbeURL() string {
	return d.probeURL
}

func (d *Detector) setState(s State) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.state = s
}
