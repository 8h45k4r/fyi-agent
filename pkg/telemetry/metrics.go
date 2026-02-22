// Copyright 2024 FYI Agent Authors. Licensed under Apache-2.0.

// Package telemetry provides metrics collection and reporting for the FYI Agent.
// It tracks connection stats, DLP events, policy evaluations, and system health
// using an in-memory ring buffer with optional export to OpenTelemetry collectors.
package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// MetricType identifies the category of a recorded metric.
type MetricType int

const (
	MetricCounter   MetricType = iota // Monotonically increasing count
	MetricGauge                       // Point-in-time value
	MetricHistogram                   // Distribution of values
)

// Metric holds a single telemetry data point with labels and timestamp.
type Metric struct {
	Name      string            `json:"name"`
	Type      MetricType        `json:"type"`
	Value     float64           `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// Collector aggregates metrics from all agent subsystems.
type Collector struct {
	mu       sync.RWMutex
	counters map[string]*atomic.Int64
	gauges   map[string]*atomic.Int64
	buffer   []Metric
	bufSize  int
	bufIdx   int
	exporter Exporter
	done     chan struct{}
}

// Exporter defines the interface for sending metrics to external systems.
type Exporter interface {
	Export(ctx context.Context, metrics []Metric) error
	Shutdown(ctx context.Context) error
}

// CollectorConfig configures the metrics collector behaviour.
type CollectorConfig struct {
	BufferSize    int           // Ring buffer capacity (default 4096)
	FlushInterval time.Duration // How often to export (default 30s)
	Exporter      Exporter      // Optional external exporter
}

// DefaultConfig returns a CollectorConfig with sensible defaults.
func DefaultConfig() CollectorConfig {
	return CollectorConfig{
		BufferSize:    4096,
		FlushInterval: 30 * time.Second,
	}
}

// NewCollector creates a Collector that records metrics in a ring buffer.
func NewCollector(cfg CollectorConfig) *Collector {
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 4096
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 30 * time.Second
	}
	c := &Collector{
		counters: make(map[string]*atomic.Int64),
		gauges:   make(map[string]*atomic.Int64),
		buffer:   make([]Metric, cfg.BufferSize),
		bufSize:  cfg.BufferSize,
		exporter: cfg.Exporter,
		done:     make(chan struct{}),
	}
	if c.exporter != nil {
		go c.flushLoop(cfg.FlushInterval)
	}
	return c
}

// IncrCounter atomically increments a named counter by delta.
func (c *Collector) IncrCounter(name string, delta int64, labels map[string]string) {
	c.mu.RLock()
	ctr, ok := c.counters[name]
	c.mu.RUnlock()

	if !ok {
		c.mu.Lock()
		ctr, ok = c.counters[name]
		if !ok {
			ctr = &atomic.Int64{}
			c.counters[name] = ctr
		}
		c.mu.Unlock()
	}
	newVal := ctr.Add(delta)
	c.record(Metric{
		Name:      name,
		Type:      MetricCounter,
		Value:     float64(newVal),
		Labels:    labels,
		Timestamp: time.Now(),
	})
}

// SetGauge atomically sets a named gauge to val.
func (c *Collector) SetGauge(name string, val int64, labels map[string]string) {
	c.mu.RLock()
	g, ok := c.gauges[name]
	c.mu.RUnlock()

	if !ok {
		c.mu.Lock()
		g, ok = c.gauges[name]
		if !ok {
			g = &atomic.Int64{}
			c.gauges[name] = g
		}
		c.mu.Unlock()
	}
	g.Store(val)
	c.record(Metric{
		Name:      name,
		Type:      MetricGauge,
		Value:     float64(val),
		Labels:    labels,
		Timestamp: time.Now(),
	})
}

// RecordHistogram records a single observation for a histogram metric.
func (c *Collector) RecordHistogram(name string, val float64, labels map[string]string) {
	c.record(Metric{
		Name:      name,
		Type:      MetricHistogram,
		Value:     val,
		Labels:    labels,
		Timestamp: time.Now(),
	})
}

// record writes a metric into the ring buffer (lock-free index via atomic).
func (c *Collector) record(m Metric) {
	c.mu.Lock()
	c.buffer[c.bufIdx%c.bufSize] = m
	c.bufIdx++
	c.mu.Unlock()
}

// Snapshot returns a copy of the most recent n metrics from the ring buffer.
func (c *Collector) Snapshot(n int) []Metric {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.bufIdx
	if total == 0 {
		return nil
	}
	if n <= 0 || n > c.bufSize {
		n = c.bufSize
	}
	if n > total {
		n = total
	}

	result := make([]Metric, n)
	start := (total - n) % c.bufSize
	for i := 0; i < n; i++ {
		result[i] = c.buffer[(start+i)%c.bufSize]
	}
	return result
}

// CounterValue returns the current value of a named counter.
func (c *Collector) CounterValue(name string) (int64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ctr, ok := c.counters[name]
	if !ok {
		return 0, false
	}
	return ctr.Load(), true
}

// GaugeValue returns the current value of a named gauge.
func (c *Collector) GaugeValue(name string) (int64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	g, ok := c.gauges[name]
	if !ok {
		return 0, false
	}
	return g.Load(), true
}

// JSON serialises the last n metrics to JSON for diagnostics.
func (c *Collector) JSON(n int) ([]byte, error) {
	snap := c.Snapshot(n)
	return json.Marshal(snap)
}

// flushLoop periodically exports buffered metrics.
func (c *Collector) flushLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			snap := c.Snapshot(c.bufSize)
			if len(snap) > 0 && c.exporter != nil {
				if err := c.exporter.Export(ctx, snap); err != nil {
					fmt.Printf("telemetry: export error: %v\n", err)
				}
			}
			cancel()
		case <-c.done:
			return
		}
	}
}

// Shutdown gracefully stops the collector and flushes remaining metrics.
func (c *Collector) Shutdown(ctx context.Context) error {
	close(c.done)
	if c.exporter != nil {
		snap := c.Snapshot(c.bufSize)
		if len(snap) > 0 {
			if err := c.exporter.Export(ctx, snap); err != nil {
				return fmt.Errorf("telemetry: final flush failed: %w", err)
			}
		}
		return c.exporter.Shutdown(ctx)
	}
	return nil
}

// Well-known metric names used across the agent.
const (
	MetricDLPScans        = "dlp.scans.total"
	MetricDLPBlocks       = "dlp.blocks.total"
	MetricPolicyEvals     = "policy.evaluations.total"
	MetricPolicyDenied    = "policy.denied.total"
	MetricTunnelBytesTx   = "tunnel.bytes.tx"
	MetricTunnelBytesRx   = "tunnel.bytes.rx"
	MetricTunnelConns     = "tunnel.connections.active"
	MetricSSLInspections  = "ssl.inspections.total"
	MetricSSLBypassed     = "ssl.bypassed.total"
	MetricCaptiveDetected = "captive.detected.total"
	MetricWatchdogRestarts = "watchdog.restarts.total"
	MetricAuthSuccess     = "auth.success.total"
	MetricAuthFailure     = "auth.failure.total"
)
