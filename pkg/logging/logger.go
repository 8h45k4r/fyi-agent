// Copyright 2024 FYI Agent Authors. Licensed under Apache-2.0.

// Package logging provides structured logging with file rotation for the FYI Agent.
// It wraps Go's log/slog with sensible defaults, log level management,
// and a rotating file writer to prevent unbounded disk usage.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// Config controls logging behaviour.
type Config struct {
	Level      string `yaml:"level"`       // debug, info, warn, error
	Format     string `yaml:"format"`      // json or text
	Output     string `yaml:"output"`      // stdout, stderr, or file path
	MaxSizeMB  int    `yaml:"max_size_mb"` // Max size per log file in MB
	MaxBackups int    `yaml:"max_backups"` // Number of old log files to keep
}

// DefaultConfig returns production-ready logging defaults.
func DefaultConfig() Config {
	return Config{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		MaxSizeMB:  50,
		MaxBackups: 5,
	}
}

// ParseLevel converts a string log level to slog.Level.
func ParseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// New creates a structured logger from the given configuration.
// Returns the logger and a cleanup function to close any open file handles.
func New(cfg Config) (*slog.Logger, func(), error) {
	level := ParseLevel(cfg.Level)
	opts := &slog.HandlerOptions{Level: level}

	writer, cleanup, err := newWriter(cfg)
	if err != nil {
		return nil, nil, err
	}

	var handler slog.Handler
	switch cfg.Format {
	case "text":
		handler = slog.NewTextHandler(writer, opts)
	default:
		handler = slog.NewJSONHandler(writer, opts)
	}

	logger := slog.New(handler)
	return logger, cleanup, nil
}

// newWriter creates the appropriate io.Writer based on output configuration.
func newWriter(cfg Config) (io.Writer, func(), error) {
	switch cfg.Output {
	case "stdout", "":
		return os.Stdout, func() {}, nil
	case "stderr":
		return os.Stderr, func() {}, nil
	default:
		// File output with rotation
		dir := filepath.Dir(cfg.Output)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, nil, fmt.Errorf("logging: create log dir: %w", err)
		}
		rw := newRotatingWriter(cfg.Output, cfg.MaxSizeMB, cfg.MaxBackups)
		return rw, func() { rw.Close() }, nil
	}
}

// rotatingWriter implements a basic log file rotation strategy.
type rotatingWriter struct {
	mu         sync.Mutex
	filepath   string
	maxBytes   int64
	maxBackups int
	file       *os.File
	size       int64
}

func newRotatingWriter(path string, maxSizeMB, maxBackups int) *rotatingWriter {
	if maxSizeMB <= 0 {
		maxSizeMB = 50
	}
	if maxBackups <= 0 {
		maxBackups = 5
	}
	rw := &rotatingWriter{
		filepath:   path,
		maxBytes:   int64(maxSizeMB) * 1024 * 1024,
		maxBackups: maxBackups,
	}
	return rw
}

func (rw *rotatingWriter) Write(p []byte) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	if rw.file == nil {
		if err := rw.openFile(); err != nil {
			return 0, err
		}
	}

	if rw.size+int64(len(p)) > rw.maxBytes {
		if err := rw.rotate(); err != nil {
			return 0, err
		}
	}

	n, err := rw.file.Write(p)
	rw.size += int64(n)
	return n, err
}

func (rw *rotatingWriter) openFile() error {
	f, err := os.OpenFile(rw.filepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("logging: open file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("logging: stat file: %w", err)
	}
	rw.file = f
	rw.size = info.Size()
	return nil
}

func (rw *rotatingWriter) rotate() error {
	if rw.file != nil {
		rw.file.Close()
		rw.file = nil
	}

	// Shift existing backups
	for i := rw.maxBackups - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", rw.filepath, i)
		dst := fmt.Sprintf("%s.%d", rw.filepath, i+1)
		os.Rename(src, dst)
	}

	// Move current to .1
	os.Rename(rw.filepath, rw.filepath+".1")

	// Remove oldest if exceeds maxBackups
	oldest := fmt.Sprintf("%s.%d", rw.filepath, rw.maxBackups+1)
	os.Remove(oldest)

	return rw.openFile()
}

// Close closes the underlying log file.
func (rw *rotatingWriter) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.file != nil {
		err := rw.file.Close()
		rw.file = nil
		return err
	}
	return nil
}

// WithComponent creates a child logger with a component name attribute.
func WithComponent(logger *slog.Logger, component string) *slog.Logger {
	return logger.With("component", component)
}
