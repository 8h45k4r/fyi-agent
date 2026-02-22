package captive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewDetector(t *testing.T) {
	d := NewDetector("", 0)
	if d == nil {
		t.Fatal("NewDetector returned nil")
	}
	if d.ProbeURL() == "" {
		t.Error("probe URL should have a default")
	}
}

func TestNewDetectorCustomURL(t *testing.T) {
	d := NewDetector("http://custom.probe/check", 10*time.Second)
	if d.ProbeURL() != "http://custom.probe/check" {
		t.Errorf("expected custom URL, got %q", d.ProbeURL())
	}
}

func TestNewDetectorDefaultTimeout(t *testing.T) {
	d := NewDetector("", 0)
	// Should use 5s default, just verify it was created
	if d == nil {
		t.Fatal("detector should not be nil")
	}
}

func TestInitialState(t *testing.T) {
	d := NewDetector("", 0)
	if d.State() != StateUnknown {
		t.Errorf("expected StateUnknown, got %d", d.State())
	}
}

func TestProbeOpenInternet(t *testing.T) {
	// Mock server returning 204 (no content) = open internet
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	d := NewDetector(server.URL, 5*time.Second)
	state, err := d.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe returned error: %v", err)
	}
	if state != StateOpen {
		t.Errorf("expected StateOpen, got %d", state)
	}
	if d.State() != StateOpen {
		t.Error("detector state should be updated")
	}
}

func TestProbeOK(t *testing.T) {
	// Mock server returning 200 OK = open internet
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	d := NewDetector(server.URL, 5*time.Second)
	state, _ := d.Probe(context.Background())
	if state != StateOpen {
		t.Errorf("expected StateOpen for 200 OK, got %d", state)
	}
}

func TestProbeCaptivePortal(t *testing.T) {
	// Mock server returning redirect = captive portal
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://portal.example.com", http.StatusFound)
	}))
	defer server.Close()

	d := NewDetector(server.URL, 5*time.Second)
	state, _ := d.Probe(context.Background())
	if state != StateCaptive {
		t.Errorf("expected StateCaptive for redirect, got %d", state)
	}
}

func TestProbeNetworkError(t *testing.T) {
	d := NewDetector("http://192.0.2.1:1", 1*time.Second)
	state, err := d.Probe(context.Background())
	if err == nil {
		t.Error("expected error for unreachable host")
	}
	if state != StateNoNetwork {
		t.Errorf("expected StateNoNetwork, got %d", state)
	}
}

func TestStateConstants(t *testing.T) {
	if StateUnknown != 0 {
		t.Error("StateUnknown should be 0")
	}
	if StateOpen != 1 {
		t.Error("StateOpen should be 1")
	}
	if StateCaptive != 2 {
		t.Error("StateCaptive should be 2")
	}
	if StateNoNetwork != 3 {
		t.Error("StateNoNetwork should be 3")
	}
}
