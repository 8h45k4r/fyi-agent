// Copyright 2024 FYI Agent Authors. Licensed under Apache-2.0.

package telemetry

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

// mockExporter captures exported metrics for test assertions.
type mockExporter struct {
	mu       sync.Mutex
	batches  [][]Metric
	shutdown bool
}

func (m *mockExporter) Export(_ context.Context, metrics []Metric) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]Metric, len(metrics))
	copy(cp, metrics)
	m.batches = append(m.batches, cp)
	return nil
}

func (m *mockExporter) Shutdown(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shutdown = true
	return nil
}

func TestNewCollectorDefaults(t *testing.T) {
	c := NewCollector(CollectorConfig{})
	if c.bufSize != 4096 {
		t.Errorf("expected default bufSize 4096, got %d", c.bufSize)
	}
	snap := c.Snapshot(10)
	if snap != nil {
		t.Errorf("expected nil snapshot on empty collector, got %d items", len(snap))
	}
}

func TestIncrCounter(t *testing.T) {
	c := NewCollector(DefaultConfig())

	c.IncrCounter("test.counter", 1, nil)
	c.IncrCounter("test.counter", 5, nil)

	val, ok := c.CounterValue("test.counter")
	if !ok {
		t.Fatal("counter not found")
	}
	if val != 6 {
		t.Errorf("expected counter value 6, got %d", val)
	}
}

func TestSetGauge(t *testing.T) {
	c := NewCollector(DefaultConfig())

	c.SetGauge("test.gauge", 42, nil)
	val, ok := c.GaugeValue("test.gauge")
	if !ok {
		t.Fatal("gauge not found")
	}
	if val != 42 {
		t.Errorf("expected gauge value 42, got %d", val)
	}

	c.SetGauge("test.gauge", 100, nil)
	val, _ = c.GaugeValue("test.gauge")
	if val != 100 {
		t.Errorf("expected gauge value 100 after update, got %d", val)
	}
}

func TestRecordHistogram(t *testing.T) {
	c := NewCollector(DefaultConfig())
	labels := map[string]string{"method": "POST"}
	c.RecordHistogram("request.latency", 0.125, labels)

	snap := c.Snapshot(1)
	if len(snap) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(snap))
	}
	if snap[0].Type != MetricHistogram {
		t.Errorf("expected histogram type, got %d", snap[0].Type)
	}
	if snap[0].Value != 0.125 {
		t.Errorf("expected value 0.125, got %f", snap[0].Value)
	}
	if snap[0].Labels["method"] != "POST" {
		t.Errorf("expected label method=POST, got %s", snap[0].Labels["method"])
	}
}

func TestSnapshotRingBuffer(t *testing.T) {
	c := NewCollector(CollectorConfig{BufferSize: 4})

	// Fill buffer beyond capacity to test ring behaviour
	for i := 0; i < 7; i++ {
		c.IncrCounter("ring.test", 1, nil)
	}

	snap := c.Snapshot(4)
	if len(snap) != 4 {
		t.Fatalf("expected 4 metrics in snapshot, got %d", len(snap))
	}
	// All metrics should have the same counter name
	for _, m := range snap {
		if m.Name != "ring.test" {
			t.Errorf("expected name ring.test, got %s", m.Name)
		}
	}
}

func TestSnapshotRequestMoreThanAvailable(t *testing.T) {
	c := NewCollector(DefaultConfig())
	c.IncrCounter("only.one", 1, nil)

	snap := c.Snapshot(100)
	if len(snap) != 1 {
		t.Errorf("expected 1 metric when requesting more than available, got %d", len(snap))
	}
}

func TestCounterValueNotFound(t *testing.T) {
	c := NewCollector(DefaultConfig())
	_, ok := c.CounterValue("nonexistent")
	if ok {
		t.Error("expected ok=false for nonexistent counter")
	}
}

func TestGaugeValueNotFound(t *testing.T) {
	c := NewCollector(DefaultConfig())
	_, ok := c.GaugeValue("nonexistent")
	if ok {
		t.Error("expected ok=false for nonexistent gauge")
	}
}

func TestJSONSerialization(t *testing.T) {
	c := NewCollector(DefaultConfig())
	c.IncrCounter(MetricDLPScans, 3, map[string]string{"scanner": "regex"})

	data, err := c.JSON(1)
	if err != nil {
		t.Fatalf("JSON() error: %v", err)
	}

	var metrics []Metric
	if err := json.Unmarshal(data, &metrics); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].Name != MetricDLPScans {
		t.Errorf("expected name %s, got %s", MetricDLPScans, metrics[0].Name)
	}
}

func TestShutdownWithExporter(t *testing.T) {
	exp := &mockExporter{}
	c := NewCollector(CollectorConfig{
		BufferSize: 64,
		Exporter:   exp,
	})
	c.IncrCounter("shutdown.test", 1, nil)

	err := c.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown() error: %v", err)
	}
	if !exp.shutdown {
		t.Error("expected exporter Shutdown to be called")
	}
	if len(exp.batches) == 0 {
		t.Error("expected at least one export batch during shutdown")
	}
}

func TestShutdownWithoutExporter(t *testing.T) {
	c := NewCollector(DefaultConfig())
	err := c.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown() without exporter should not error: %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := NewCollector(CollectorConfig{BufferSize: 256})
	var wg sync.WaitGroup

	// Concurrent counter increments
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.IncrCounter("concurrent.counter", 1, nil)
		}()
	}

	// Concurrent gauge sets
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			c.SetGauge("concurrent.gauge", int64(v), nil)
		}(i)
	}

	// Concurrent histogram records
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			c.RecordHistogram("concurrent.hist", float64(v), nil)
		}(i)
	}

	wg.Wait()

	val, ok := c.CounterValue("concurrent.counter")
	if !ok {
		t.Fatal("concurrent counter not found")
	}
	if val != 100 {
		t.Errorf("expected counter value 100, got %d", val)
	}

	snap := c.Snapshot(256)
	if len(snap) != 200 {
		t.Errorf("expected 200 total metrics, got %d", len(snap))
	}
}

func TestMetricTypeConstants(t *testing.T) {
	if MetricCounter != 0 {
		t.Errorf("MetricCounter should be 0, got %d", MetricCounter)
	}
	if MetricGauge != 1 {
		t.Errorf("MetricGauge should be 1, got %d", MetricGauge)
	}
	if MetricHistogram != 2 {
		t.Errorf("MetricHistogram should be 2, got %d", MetricHistogram)
	}
}

func TestWellKnownMetricNames(t *testing.T) {
	names := []string{
		MetricDLPScans, MetricDLPBlocks,
		MetricPolicyEvals, MetricPolicyDenied,
		MetricTunnelBytesTx, MetricTunnelBytesRx, MetricTunnelConns,
		MetricSSLInspections, MetricSSLBypassed,
		MetricCaptiveDetected, MetricWatchdogRestarts,
		MetricAuthSuccess, MetricAuthFailure,
	}
	for _, n := range names {
		if n == "" {
			t.Error("well-known metric name should not be empty")
		}
	}
}
