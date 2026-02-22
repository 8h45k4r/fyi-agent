// Package dlp provides inline data loss prevention scanning for the FYI Agent.
package dlp

import (
	"fmt"
	"regexp"
	"sync"
)

// Severity represents the severity level of a DLP finding.
type Severity int

const (
	SeverityLow    Severity = iota
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

// Finding represents a detected DLP violation.
type Finding struct {
	PatternName string   `json:"pattern_name"`
	Severity    Severity `json:"severity"`
	Match       string   `json:"match"`
	Redacted    string   `json:"redacted"`
	Offset      int      `json:"offset"`
}

// Pattern defines a DLP detection pattern with optional validation.
type Pattern struct {
	Name      string
	Regex     *regexp.Regexp
	Severity  Severity
	Validator func(match string) bool // Optional: reduces false positives
}

// Engine is the core DLP scanning engine.
type Engine struct {
	mu       sync.RWMutex
	patterns []Pattern
	enabled  bool
}

// NewEngine creates a new DLP engine.
func NewEngine() *Engine {
	return &Engine{
		patterns: make([]Pattern, 0),
		enabled:  true,
	}
}

// AddPattern registers a new detection pattern.
func (e *Engine) AddPattern(name string, regex string, severity Severity, validator func(string) bool) error {
	if name == "" {
		return fmt.Errorf("pattern name cannot be empty")
	}
	if regex == "" {
		return fmt.Errorf("pattern regex cannot be empty")
	}

	compiled, err := regexp.Compile(regex)
	if err != nil {
		return fmt.Errorf("compiling regex for pattern %s: %w", name, err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.patterns = append(e.patterns, Pattern{
		Name:      name,
		Regex:     compiled,
		Severity:  severity,
		Validator: validator,
	})

	return nil
}

// Scan checks input data against all registered patterns and returns findings.
func (e *Engine) Scan(data []byte) []Finding {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.enabled || len(data) == 0 {
		return nil
	}

	var findings []Finding
	text := string(data)

	for _, p := range e.patterns {
		matches := p.Regex.FindAllStringIndex(text, -1)
		for _, loc := range matches {
			match := text[loc[0]:loc[1]]

			// Run validator if present to reduce false positives
			if p.Validator != nil && !p.Validator(match) {
				continue
			}

			findings = append(findings, Finding{
				PatternName: p.Name,
				Severity:    p.Severity,
				Match:       match,
				Redacted:    redact(match),
				Offset:      loc[0],
			})
		}
	}

	return findings
}

// SetEnabled enables or disables the DLP engine.
func (e *Engine) SetEnabled(enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enabled = enabled
}

// IsEnabled returns whether the DLP engine is active.
func (e *Engine) IsEnabled() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.enabled
}

// PatternCount returns the number of registered patterns.
func (e *Engine) PatternCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.patterns)
}

// redact masks a matched string, showing only the first and last 2 characters.
func redact(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	runes := []rune(s)
	masked := make([]rune, len(runes))
	for i := range runes {
		if i < 2 || i >= len(runes)-2 {
			masked[i] = runes[i]
		} else {
			masked[i] = '*'
		}
	}
	return string(masked)
}
