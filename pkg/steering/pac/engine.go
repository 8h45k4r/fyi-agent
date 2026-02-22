// Package pac implements PAC-based traffic steering for the FYI Agent.
package pac

import (
	"fmt"
	"strings"
	"sync"
)

// Action defines the routing action for matched traffic.
type Action int

const (
	ActionDirect   Action = iota // Bypass proxy, go direct
	ActionProxy                  // Route through FYI cloud proxy
	ActionBlock                  // Block the connection
	ActionTunnel                 // Route through OpenZiti mesh
)

// Rule defines a single PAC steering rule.
type Rule struct {
	Pattern  string // Domain pattern (supports * wildcard)
	Action   Action
	Priority int // Higher priority rules are evaluated first
}

// Engine manages PAC-based traffic steering decisions.
type Engine struct {
	mu    sync.RWMutex
	rules []Rule
}

// NewEngine creates a new PAC steering engine.
func NewEngine() *Engine {
	return &Engine{
		rules: make([]Rule, 0),
	}
}

// AddRule registers a new steering rule.
func (e *Engine) AddRule(pattern string, action Action, priority int) error {
	if pattern == "" {
		return fmt.Errorf("rule pattern cannot be empty")
	}
	if priority < 0 {
		return fmt.Errorf("rule priority cannot be negative")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	rule := Rule{Pattern: pattern, Action: action, Priority: priority}

	// Insert in priority order (highest first)
	inserted := false
	for i, existing := range e.rules {
		if priority > existing.Priority {
			e.rules = append(e.rules[:i+1], e.rules[i:]...)
			e.rules[i] = rule
			inserted = true
			break
		}
	}
	if !inserted {
		e.rules = append(e.rules, rule)
	}

	return nil
}

// Evaluate determines the routing action for a given hostname.
func (e *Engine) Evaluate(hostname string) Action {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if hostname == "" {
		return ActionDirect
	}

	hostname = strings.ToLower(hostname)

	for _, rule := range e.rules {
		if matchPattern(rule.Pattern, hostname) {
			return rule.Action
		}
	}

	// Default: route through proxy
	return ActionProxy
}

// RuleCount returns the number of registered rules.
func (e *Engine) RuleCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.rules)
}

// ClearRules removes all rules.
func (e *Engine) ClearRules() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = make([]Rule, 0)
}

// matchPattern checks if a hostname matches a PAC-style pattern.
// Supports * as wildcard prefix (e.g., "*.example.com").
func matchPattern(pattern, hostname string) bool {
	pattern = strings.ToLower(pattern)

	if pattern == "*" {
		return true
	}

	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // Keep the dot: ".example.com"
		return strings.HasSuffix(hostname, suffix) || hostname == pattern[2:]
	}

	return pattern == hostname
}
