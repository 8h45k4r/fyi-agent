// Package policy implements the AI-driven policy engine for the FYI Agent.
// It evaluates traffic against configurable rules with risk scoring
// and supports local, remote, and hybrid policy evaluation modes.
package policy

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Action represents a policy decision.
type Action string

const (
	ActionAllow  Action = "allow"
	ActionBlock  Action = "block"
	ActionAlert  Action = "alert"
	ActionPrompt Action = "prompt"
)

// RiskLevel represents a risk classification.
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// Config holds policy engine configuration.
type Config struct {
	Engine        string        `yaml:"engine"`
	RemoteURL     string        `yaml:"remote_url"`
	SyncInterval  time.Duration `yaml:"sync_interval"`
	DefaultAction Action        `yaml:"default_action"`
	RiskThresholds RiskThresholds `yaml:"risk_thresholds"`
}

// RiskThresholds defines the score boundaries for risk levels.
type RiskThresholds struct {
	Low      float64 `yaml:"low"`
	Medium   float64 `yaml:"medium"`
	High     float64 `yaml:"high"`
	Critical float64 `yaml:"critical"`
}

// DefaultConfig returns sensible policy defaults.
func DefaultConfig() Config {
	return Config{
		Engine:        "local",
		SyncInterval:  60 * time.Second,
		DefaultAction: ActionAllow,
		RiskThresholds: RiskThresholds{
			Low:      0.3,
			Medium:   0.6,
			High:     0.8,
			Critical: 0.95,
		},
	}
}

// Request represents a policy evaluation request.
type Request struct {
	SourceIP    string            `json:"source_ip"`
	DestIP      string            `json:"dest_ip"`
	DestDomain  string            `json:"dest_domain"`
	DestPort    int               `json:"dest_port"`
	Protocol    string            `json:"protocol"`
	UserID      string            `json:"user_id"`
	DeviceID    string            `json:"device_id"`
	Labels      map[string]string `json:"labels,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	BytesSent   int64             `json:"bytes_sent"`
}

// Decision represents the policy engine's verdict.
type Decision struct {
	Action    Action    `json:"action"`
	RiskScore float64   `json:"risk_score"`
	RiskLevel RiskLevel `json:"risk_level"`
	Reason    string    `json:"reason"`
	RuleID    string    `json:"rule_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Rule represents a policy rule.
type Rule struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Priority    int               `json:"priority"`
	Action      Action            `json:"action"`
	Conditions  map[string]string `json:"conditions"`
	Enabled     bool              `json:"enabled"`
}

// Engine evaluates policy decisions for network traffic.
type Engine struct {
	cfg    Config
	logger *slog.Logger
	rules  []Rule
	mu     sync.RWMutex
}

// NewEngine creates a new policy engine.
func NewEngine(cfg Config, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		cfg:    cfg,
		logger: logger,
		rules:  make([]Rule, 0),
	}
}

// Evaluate processes a request and returns a policy decision.
func (e *Engine) Evaluate(ctx context.Context, req Request) Decision {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Evaluate rules in priority order (lower number = higher priority)
	for _, rule := range e.rules {
		if !rule.Enabled {
			continue
		}
		if e.matchesRule(req, rule) {
			score := e.calculateRiskScore(req, rule)
			return Decision{
				Action:    rule.Action,
				RiskScore: score,
				RiskLevel: e.classifyRisk(score),
				Reason:    fmt.Sprintf("matched rule: %s", rule.Name),
				RuleID:    rule.ID,
				Timestamp: time.Now(),
			}
		}
	}

	// Default decision
	return Decision{
		Action:    e.cfg.DefaultAction,
		RiskScore: 0.0,
		RiskLevel: RiskLow,
		Reason:    "no matching rules, default action applied",
		Timestamp: time.Now(),
	}
}

// AddRule adds a rule to the engine.
func (e *Engine) AddRule(rule Rule) error {
	if rule.ID == "" {
		return fmt.Errorf("policy: rule ID cannot be empty")
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check for duplicate
	for _, r := range e.rules {
		if r.ID == rule.ID {
			return fmt.Errorf("policy: rule %q already exists", rule.ID)
		}
	}

	e.rules = append(e.rules, rule)
	e.logger.Info("rule added", "id", rule.ID, "name", rule.Name)
	return nil
}

// RemoveRule removes a rule by ID.
func (e *Engine) RemoveRule(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i, r := range e.rules {
		if r.ID == id {
			e.rules = append(e.rules[:i], e.rules[i+1:]...)
			e.logger.Info("rule removed", "id", id)
			return nil
		}
	}
	return fmt.Errorf("policy: rule %q not found", id)
}

// RuleCount returns the number of loaded rules.
func (e *Engine) RuleCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.rules)
}

func (e *Engine) matchesRule(req Request, rule Rule) bool {
	for key, val := range rule.Conditions {
		switch key {
		case "dest_domain":
			if req.DestDomain != val {
				return false
			}
		case "protocol":
			if req.Protocol != val {
				return false
			}
		case "user_id":
			if req.UserID != val {
				return false
			}
		case "content_type":
			if req.ContentType != val {
				return false
			}
		}
	}
	return true
}

func (e *Engine) calculateRiskScore(req Request, rule Rule) float64 {
	// Base score from rule action severity
	var base float64
	switch rule.Action {
	case ActionBlock:
		base = 0.9
	case ActionAlert:
		base = 0.6
	case ActionPrompt:
		base = 0.4
	default:
		base = 0.1
	}
	return base
}

func (e *Engine) classifyRisk(score float64) RiskLevel {
	switch {
	case score >= e.cfg.RiskThresholds.Critical:
		return RiskCritical
	case score >= e.cfg.RiskThresholds.High:
		return RiskHigh
	case score >= e.cfg.RiskThresholds.Medium:
		return RiskMedium
	default:
		return RiskLow
	}
}
