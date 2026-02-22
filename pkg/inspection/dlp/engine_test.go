package dlp

import (
	"testing"
)

func TestNewEngine(t *testing.T) {
	e := NewEngine()
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	if !e.IsEnabled() {
		t.Error("engine should be enabled by default")
	}
	if e.PatternCount() != 0 {
		t.Error("new engine should have zero patterns")
	}
}

func TestAddPattern(t *testing.T) {
	e := NewEngine()

	err := e.AddPattern("ssn", `\d{3}-\d{2}-\d{4}`, SeverityHigh, nil)
	if err != nil {
		t.Fatalf("AddPattern returned error: %v", err)
	}
	if e.PatternCount() != 1 {
		t.Errorf("expected 1 pattern, got %d", e.PatternCount())
	}
}

func TestAddPatternEmptyName(t *testing.T) {
	e := NewEngine()
	err := e.AddPattern("", `\d+`, SeverityLow, nil)
	if err == nil {
		t.Error("expected error for empty pattern name")
	}
}

func TestAddPatternEmptyRegex(t *testing.T) {
	e := NewEngine()
	err := e.AddPattern("test", "", SeverityLow, nil)
	if err == nil {
		t.Error("expected error for empty regex")
	}
}

func TestAddPatternInvalidRegex(t *testing.T) {
	e := NewEngine()
	err := e.AddPattern("bad", "[invalid", SeverityLow, nil)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestScanFindsPattern(t *testing.T) {
	e := NewEngine()
	_ = e.AddPattern("ssn", `\d{3}-\d{2}-\d{4}`, SeverityHigh, nil)

	findings := e.Scan([]byte("My SSN is 123-45-6789 please process"))
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].PatternName != "ssn" {
		t.Errorf("expected pattern name 'ssn', got %q", findings[0].PatternName)
	}
	if findings[0].Severity != SeverityHigh {
		t.Errorf("expected severity High, got %v", findings[0].Severity)
	}
	if findings[0].Match != "123-45-6789" {
		t.Errorf("expected match '123-45-6789', got %q", findings[0].Match)
	}
}

func TestScanNoMatch(t *testing.T) {
	e := NewEngine()
	_ = e.AddPattern("ssn", `\d{3}-\d{2}-\d{4}`, SeverityHigh, nil)

	findings := e.Scan([]byte("no sensitive data here"))
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
}

func TestScanEmptyData(t *testing.T) {
	e := NewEngine()
	_ = e.AddPattern("ssn", `\d{3}-\d{2}-\d{4}`, SeverityHigh, nil)

	findings := e.Scan([]byte{})
	if findings != nil {
		t.Errorf("expected nil findings for empty data, got %v", findings)
	}
}

func TestScanDisabledEngine(t *testing.T) {
	e := NewEngine()
	_ = e.AddPattern("ssn", `\d{3}-\d{2}-\d{4}`, SeverityHigh, nil)
	e.SetEnabled(false)

	findings := e.Scan([]byte("SSN: 123-45-6789"))
	if findings != nil {
		t.Error("disabled engine should return nil findings")
	}
}

func TestScanWithValidator(t *testing.T) {
	e := NewEngine()

	// Validator that rejects matches starting with "000"
	validator := func(match string) bool {
		return match[:3] != "000"
	}
	_ = e.AddPattern("ssn", `\d{3}-\d{2}-\d{4}`, SeverityHigh, validator)

	// Valid SSN
	findings := e.Scan([]byte("SSN: 123-45-6789"))
	if len(findings) != 1 {
		t.Errorf("expected 1 finding for valid SSN, got %d", len(findings))
	}

	// Invalid SSN (starts with 000)
	findings = e.Scan([]byte("SSN: 000-45-6789"))
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for invalid SSN, got %d", len(findings))
	}
}

func TestScanMultipleMatches(t *testing.T) {
	e := NewEngine()
	_ = e.AddPattern("ssn", `\d{3}-\d{2}-\d{4}`, SeverityHigh, nil)

	findings := e.Scan([]byte("SSN1: 123-45-6789 SSN2: 987-65-4321"))
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
}

func TestRedaction(t *testing.T) {
	e := NewEngine()
	_ = e.AddPattern("ssn", `\d{3}-\d{2}-\d{4}`, SeverityHigh, nil)

	findings := e.Scan([]byte("SSN: 123-45-6789"))
	if len(findings) != 1 {
		t.Fatal("expected 1 finding")
	}
	redacted := findings[0].Redacted
	// Should show first 2 and last 2 chars
	if len(redacted) != len("123-45-6789") {
		t.Errorf("redacted length mismatch: got %d", len(redacted))
	}
}

func TestSetEnabled(t *testing.T) {
	e := NewEngine()

	if !e.IsEnabled() {
		t.Error("should be enabled by default")
	}
	e.SetEnabled(false)
	if e.IsEnabled() {
		t.Error("should be disabled after SetEnabled(false)")
	}
	e.SetEnabled(true)
	if !e.IsEnabled() {
		t.Error("should be enabled after SetEnabled(true)")
	}
}

func TestSeverityLevels(t *testing.T) {
	if SeverityLow >= SeverityMedium {
		t.Error("SeverityLow should be less than SeverityMedium")
	}
	if SeverityMedium >= SeverityHigh {
		t.Error("SeverityMedium should be less than SeverityHigh")
	}
	if SeverityHigh >= SeverityCritical {
		t.Error("SeverityHigh should be less than SeverityCritical")
	}
}
