// Copyright 2024 FYI Agent Authors. Licensed under Apache-2.0.

package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		local, remote string
		want          bool
	}{
		{"1.0.0", "1.0.1", true},
		{"1.0.0", "1.1.0", true},
		{"1.0.0", "2.0.0", true},
		{"v1.0.0", "v1.0.1", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.1", "1.0.0", false},
		{"2.0.0", "1.9.9", false},
		{"1.0", "1.0.1", true},
		{"1.0.1", "1.0", false},
	}
	for _, tt := range tests {
		got := isNewer(tt.local, tt.remote)
		if got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.local, tt.remote, got, tt.want)
		}
	}
}

func TestNewDefaults(t *testing.T) {
	u := New(Config{CurrentVersion: "1.0.0", CheckURL: "http://localhost/update"})
	if u.cfg.CheckInterval != 1*time.Hour {
		t.Errorf("expected default interval 1h, got %v", u.cfg.CheckInterval)
	}
	if u.cfg.StagingDir == "" {
		t.Error("expected non-empty staging dir")
	}
	if u.client == nil {
		t.Error("expected non-nil HTTP client")
	}
}

func TestCheckForUpdateNewerAvailable(t *testing.T) {
	release := ReleaseInfo{
		Version: "2.0.0",
		Assets: map[string]Asset{
			"linux/amd64": {URL: "http://example.com/fyi-agent", SHA256: "abc123"},
		},
		Published: time.Now(),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(release)
	}))
	defer srv.Close()

	u := New(Config{
		CurrentVersion: "1.0.0",
		CheckURL:       srv.URL,
		HTTPClient:     srv.Client(),
	})

	got, err := u.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate() error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil release for newer version")
	}
	if got.Version != "2.0.0" {
		t.Errorf("expected version 2.0.0, got %s", got.Version)
	}
}

func TestCheckForUpdateAlreadyCurrent(t *testing.T) {
	release := ReleaseInfo{Version: "1.0.0"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(release)
	}))
	defer srv.Close()

	u := New(Config{
		CurrentVersion: "1.0.0",
		CheckURL:       srv.URL,
		HTTPClient:     srv.Client(),
	})

	got, err := u.CheckForUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdate() error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil when already up to date, got %+v", got)
	}
}

func TestCheckForUpdateServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	u := New(Config{
		CurrentVersion: "1.0.0",
		CheckURL:       srv.URL,
		HTTPClient:     srv.Client(),
	})

	_, err := u.CheckForUpdate(context.Background())
	if err == nil {
		t.Fatal("expected error for server 500")
	}
}

func TestDownloadWithChecksum(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho hello")
	hash := sha256.Sum256(binaryContent)
	hashStr := hex.EncodeToString(hash[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(binaryContent)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	platformKey := runtime.GOOS + "/" + runtime.GOARCH

	release := &ReleaseInfo{
		Version: "2.0.0",
		Assets: map[string]Asset{
			platformKey: {
				URL:      srv.URL + "/download",
				SHA256:   hashStr,
				Filename: "fyi-agent-new",
			},
		},
	}

	u := New(Config{
		CurrentVersion: "1.0.0",
		CheckURL:       srv.URL,
		StagingDir:     tmpDir,
		HTTPClient:     srv.Client(),
	})

	path, err := u.Download(context.Background(), release)
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if string(data) != string(binaryContent) {
		t.Error("downloaded content does not match")
	}
}

func TestDownloadChecksumMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("corrupted data"))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	platformKey := runtime.GOOS + "/" + runtime.GOARCH

	release := &ReleaseInfo{
		Version: "2.0.0",
		Assets: map[string]Asset{
			platformKey: {
				URL:      srv.URL,
				SHA256:   "0000000000000000000000000000000000000000000000000000000000000000",
				Filename: "fyi-agent-bad",
			},
		},
	}

	u := New(Config{
		CurrentVersion: "1.0.0",
		StagingDir:     tmpDir,
		HTTPClient:     srv.Client(),
	})

	_, err := u.Download(context.Background(), release)
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}

	// Verify corrupted file was cleaned up
	path := filepath.Join(tmpDir, "fyi-agent-bad")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected corrupted file to be removed")
	}
}

func TestDownloadNoPlatformAsset(t *testing.T) {
	release := &ReleaseInfo{
		Version: "2.0.0",
		Assets:  map[string]Asset{"fake/arch": {URL: "http://x"}},
	}

	u := New(Config{CurrentVersion: "1.0.0"})
	_, err := u.Download(context.Background(), release)
	if err == nil {
		t.Fatal("expected error for missing platform")
	}
}

func TestLastRelease(t *testing.T) {
	u := New(Config{CurrentVersion: "1.0.0", CheckURL: "http://localhost"})
	if u.LastRelease() != nil {
		t.Error("expected nil last release initially")
	}
}

func TestStopDoesNotPanic(t *testing.T) {
	u := New(Config{CurrentVersion: "1.0.0", CheckURL: "http://localhost"})
	u.Stop() // should not panic
}
