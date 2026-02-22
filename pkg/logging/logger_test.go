// Copyright 2024 FYI Agent Authors. Licensed under Apache-2.0.

package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Level != "info" {
		t.Errorf("expected level info, got %s", cfg.Level)
	}
	if cfg.Format != "json" {
		t.Errorf("expected format json, got %s", cfg.Format)
	}
	if cfg.Output != "stdout" {
		t.Errorf("expected output stdout, got %s", cfg.Output)
	}
	if cfg.MaxSizeMB != 50 {
		t.Errorf("expected MaxSizeMB 50, got %d", cfg.MaxSizeMB)
	}
	if cfg.MaxBackups != 5 {
		t.Errorf("expected MaxBackups 5, got %d", cfg.MaxBackups)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"unknown", slog.LevelInfo},
		{"", slog.LevelInfo},
	}
	for _, tt := range tests {
		got := ParseLevel(tt.input)
		if got != tt.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestNewStdoutJSON(t *testing.T) {
	logger, cleanup, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer cleanup()
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestNewStdoutText(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Format = "text"
	logger, cleanup, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer cleanup()
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestNewStderr(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Output = "stderr"
	logger, cleanup, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer cleanup()
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestNewFileOutput(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	cfg := Config{
		Level:      "debug",
		Format:     "json",
		Output:     logPath,
		MaxSizeMB:  1,
		MaxBackups: 2,
	}

	logger, cleanup, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer cleanup()

	logger.Info("test message", "key", "value")

	// Verify file was created
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("expected log file to be created")
	}
}

func TestRotatingWriterBasic(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "rotate.log")

	rw := newRotatingWriter(path, 1, 2) // 1MB max, 2 backups
	defer rw.Close()

	// Write some data
	data := []byte("hello world\n")
	n, err := rw.Write(data)
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected %d bytes written, got %d", len(data), n)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected log file to exist")
	}
}

func TestRotatingWriterRotation(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "rotate.log")

	// Very small max size to trigger rotation quickly
	rw := newRotatingWriter(path, 0, 2) // Uses default 50MB
	// Override maxBytes directly for testing
	rw.maxBytes = 100 // 100 bytes
	defer rw.Close()

	// Write enough data to trigger rotation
	data := strings.Repeat("x", 60) + "\n"
	rw.Write([]byte(data)) // 61 bytes
	rw.Write([]byte(data)) // Would exceed 100, triggers rotation

	// Check that backup file was created
	backup := path + ".1"
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		t.Error("expected backup file .1 to exist after rotation")
	}
}

func TestRotatingWriterDefaults(t *testing.T) {
	rw := newRotatingWriter("/tmp/test.log", 0, 0)
	if rw.maxBytes != 50*1024*1024 {
		t.Errorf("expected default 50MB, got %d", rw.maxBytes)
	}
	if rw.maxBackups != 5 {
		t.Errorf("expected default 5 backups, got %d", rw.maxBackups)
	}
}

func TestWithComponent(t *testing.T) {
	logger, cleanup, _ := New(DefaultConfig())
	defer cleanup()

	child := WithComponent(logger, "tunnel")
	if child == nil {
		t.Fatal("expected non-nil child logger")
	}
}
