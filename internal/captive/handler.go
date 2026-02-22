package captive

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// HandlerConfig configures the captive portal handler behavior.
type HandlerConfig struct {
	// ListenAddr is the address to serve the captive portal redirect page.
	ListenAddr string `yaml:"listen_addr" json:"listen_addr"`
	// RedirectURL is where to send users when a portal is detected.
	RedirectURL string `yaml:"redirect_url" json:"redirect_url"`
	// PauseDuration is how long to pause traffic steering during portal auth.
	PauseDuration time.Duration `yaml:"pause_duration" json:"pause_duration"`
	// AutoResume enables automatic resumption after portal auth completes.
	AutoResume bool `yaml:"auto_resume" json:"auto_resume"`
}

// DefaultHandlerConfig returns default handler settings.
func DefaultHandlerConfig() HandlerConfig {
	return HandlerConfig{
		ListenAddr:    "127.0.0.1:18080",
		PauseDuration: 5 * time.Minute,
		AutoResume:    true,
	}
}

// Handler manages the response to captive portal detection.
// It pauses tunnel traffic and provides a user-facing notification
// so the user can authenticate with the portal.
type Handler struct {
	mu        sync.RWMutex
	config    HandlerConfig
	detector  *Detector
	logger    *slog.Logger
	server    *http.Server
	paused    bool
	pausedAt  time.Time
	callbacks []func(paused bool)
	stopCh    chan struct{}
}

// NewHandler creates a captive portal handler.
func NewHandler(config HandlerConfig, detector *Detector, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		config:   config,
		detector: detector,
		logger:   logger.With("component", "captive-handler"),
		stopCh:   make(chan struct{}),
	}
}

// OnStateChange registers a callback for pause/resume events.
func (h *Handler) OnStateChange(cb func(paused bool)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.callbacks = append(h.callbacks, cb)
}

// IsPaused returns whether traffic steering is currently paused.
func (h *Handler) IsPaused() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.paused
}

// Start begins the captive portal handler.
func (h *Handler) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/captive", h.handleCaptivePage)
	mux.HandleFunc("/captive/status", h.handleStatus)
	mux.HandleFunc("/captive/resume", h.handleResume)

	h.server = &http.Server{
		Addr:    h.config.ListenAddr,
		Handler: mux,
	}

	h.logger.Info("captive portal handler starting",
		"listen", h.config.ListenAddr,
	)

	// Start monitoring for captive portals.
	go h.monitorLoop(ctx)

	go func() {
		if err := h.server.ListenAndServe(); err != http.ErrServerClosed {
			h.logger.Error("captive portal server error", "error", err)
		}
	}()

	return nil
}

// Stop shuts down the handler.
func (h *Handler) Stop() error {
	close(h.stopCh)

	if h.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return h.server.Shutdown(ctx)
	}
	return nil
}

// Pause temporarily disables traffic steering for portal authentication.
func (h *Handler) Pause() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.paused {
		return
	}

	h.paused = true
	h.pausedAt = time.Now()
	h.logger.Info("traffic steering paused for captive portal")

	for _, cb := range h.callbacks {
		go cb(true)
	}
}

// Resume re-enables traffic steering after portal authentication.
func (h *Handler) Resume() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.paused {
		return
	}

	h.paused = false
	duration := time.Since(h.pausedAt)
	h.logger.Info("traffic steering resumed",
		"paused_duration", duration,
	)

	for _, cb := range h.callbacks {
		go cb(false)
	}
}

// monitorLoop watches for captive portal state changes.
func (h *Handler) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopCh:
			return
		case <-ticker.C:
			detected, _ := h.detector.Check(ctx)
			if detected && !h.IsPaused() {
				h.Pause()
			} else if !detected && h.IsPaused() && h.config.AutoResume {
				h.Resume()
			}

			// Auto-resume after timeout.
			h.mu.RLock()
			if h.paused && time.Since(h.pausedAt) > h.config.PauseDuration {
				h.mu.RUnlock()
				h.Resume()
			} else {
				h.mu.RUnlock()
			}
		}
	}
}

// handleCaptivePage serves a user-facing page about the captive portal.
func (h *Handler) handleCaptivePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>FYI Agent - Captive Portal Detected</title></head>
<body>
<h1>Captive Portal Detected</h1>
<p>A captive portal has been detected on this network.</p>
<p>Traffic steering is temporarily paused. Please complete portal authentication.</p>
<p><a href="/captive/resume">Click here to resume</a> after authenticating.</p>
</body></html>`)
}

// handleStatus returns the current captive portal status as JSON.
func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	paused := h.IsPaused()
	fmt.Fprintf(w, `{"paused":%t}`, paused)
}

// handleResume manually resumes traffic steering.
func (h *Handler) handleResume(w http.ResponseWriter, r *http.Request) {
	h.Resume()
	http.Redirect(w, r, "/captive", http.StatusSeeOther)
}
