// Copyright 2024 FYI Agent Authors. Licensed under Apache-2.0.

// Package updater provides automatic self-update functionality for the FYI Agent.
// It periodically checks a release endpoint for newer versions, downloads the
// binary with checksum verification, and performs an atomic swap + restart.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ReleaseInfo describes a published release returned by the update server.
type ReleaseInfo struct {
	Version   string            `json:"version"`
	Assets    map[string]Asset  `json:"assets"` // keyed by "GOOS/GOARCH"
	Changelog string            `json:"changelog,omitempty"`
	Published time.Time         `json:"published"`
}

// Asset is a downloadable binary with its checksum.
type Asset struct {
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Filename string `json:"filename"`
}

// Config controls how the updater behaves.
type Config struct {
	CurrentVersion string        // Semantic version of the running binary
	CheckURL       string        // URL returning JSON ReleaseInfo
	CheckInterval  time.Duration // How often to poll (default 1h)
	StagingDir     string        // Temp directory for downloads
	HTTPClient     *http.Client  // Optional custom HTTP client
}

// Updater manages the lifecycle of automatic updates.
type Updater struct {
	cfg    Config
	client *http.Client
	mu     sync.Mutex
	done   chan struct{}
	last   *ReleaseInfo
}

// New creates an Updater with the given configuration.
func New(cfg Config) *Updater {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 1 * time.Hour
	}
	if cfg.StagingDir == "" {
		cfg.StagingDir = os.TempDir()
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Updater{
		cfg:    cfg,
		client: client,
		done:   make(chan struct{}),
	}
}

// CheckForUpdate fetches the latest release info from the server and returns
// it if a newer version is available. Returns nil if already up to date.
func (u *Updater) CheckForUpdate(ctx context.Context) (*ReleaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.cfg.CheckURL, nil)
	if err != nil {
		return nil, fmt.Errorf("updater: build request: %w", err)
	}
	req.Header.Set("User-Agent", "fyi-agent/"+u.cfg.CurrentVersion)

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("updater: check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("updater: server returned %d", resp.StatusCode)
	}

	var release ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("updater: decode release: %w", err)
	}

	u.mu.Lock()
	u.last = &release
	u.mu.Unlock()

	if !isNewer(u.cfg.CurrentVersion, release.Version) {
		return nil, nil // already up to date
	}
	return &release, nil
}

// Download fetches the binary asset for the current platform and verifies
// its SHA-256 checksum. Returns the path to the staged file.
func (u *Updater) Download(ctx context.Context, release *ReleaseInfo) (string, error) {
	platformKey := runtime.GOOS + "/" + runtime.GOARCH
	asset, ok := release.Assets[platformKey]
	if !ok {
		return "", fmt.Errorf("updater: no asset for platform %s", platformKey)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", fmt.Errorf("updater: build download request: %w", err)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("updater: download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("updater: download returned %d", resp.StatusCode)
	}

	stagePath := filepath.Join(u.cfg.StagingDir, asset.Filename)
	f, err := os.Create(stagePath)
	if err != nil {
		return "", fmt.Errorf("updater: create staging file: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		os.Remove(stagePath)
		return "", fmt.Errorf("updater: write failed: %w", err)
	}

	gotHash := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(gotHash, asset.SHA256) {
		os.Remove(stagePath)
		return "", fmt.Errorf("updater: checksum mismatch: got %s, want %s", gotHash, asset.SHA256)
	}

	// Make the binary executable
	if err := os.Chmod(stagePath, 0755); err != nil {
		os.Remove(stagePath)
		return "", fmt.Errorf("updater: chmod failed: %w", err)
	}

	return stagePath, nil
}

// Apply atomically replaces the running binary with the downloaded update.
// It renames the current binary to a backup, then moves the new binary in place.
func (u *Updater) Apply(stagedPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("updater: resolve executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("updater: eval symlinks: %w", err)
	}

	backup := exe + ".bak"

	// Remove old backup if it exists
	os.Remove(backup)

	// Rename current → backup
	if err := os.Rename(exe, backup); err != nil {
		return fmt.Errorf("updater: backup current binary: %w", err)
	}

	// Move staged → current
	if err := os.Rename(stagedPath, exe); err != nil {
		// Try to restore from backup
		os.Rename(backup, exe)
		return fmt.Errorf("updater: install new binary: %w", err)
	}

	return nil
}

// Rollback restores the previous binary version from the backup file.
func (u *Updater) Rollback() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("updater: resolve executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("updater: eval symlinks: %w", err)
	}

	backup := exe + ".bak"
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		return fmt.Errorf("updater: no backup found at %s", backup)
	}

	os.Remove(exe)
	if err := os.Rename(backup, exe); err != nil {
		return fmt.Errorf("updater: rollback failed: %w", err)
	}
	return nil
}

// LastRelease returns the most recently fetched release info.
func (u *Updater) LastRelease() *ReleaseInfo {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.last
}

// StartBackground begins a background goroutine that periodically checks
// for updates. It does not auto-apply; the caller should handle the update.
func (u *Updater) StartBackground(onUpdate func(*ReleaseInfo)) {
	go func() {
		ticker := time.NewTicker(u.cfg.CheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				release, err := u.CheckForUpdate(ctx)
				cancel()
				if err == nil && release != nil && onUpdate != nil {
					onUpdate(release)
				}
			case <-u.done:
				return
			}
		}
	}()
}

// Stop terminates the background update checker.
func (u *Updater) Stop() {
	close(u.done)
}

// isNewer returns true if remote version string is greater than local.
// Uses simple lexicographic comparison on semver-style strings.
func isNewer(local, remote string) bool {
	local = strings.TrimPrefix(local, "v")
	remote = strings.TrimPrefix(remote, "v")
	if local == remote {
		return false
	}
	// Split into parts and compare numerically where possible
	lParts := strings.Split(local, ".")
	rParts := strings.Split(remote, ".")
	for i := 0; i < len(lParts) && i < len(rParts); i++ {
		if rParts[i] > lParts[i] {
			return true
		}
		if rParts[i] < lParts[i] {
			return false
		}
	}
	return len(rParts) > len(lParts)
}
