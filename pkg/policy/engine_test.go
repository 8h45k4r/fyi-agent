package policy

import (
	"context"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Engine != "local" {
		t.Errorf("expected engine 'local', got %q", cfg.Engine)
	}
	if cfg.DefaultAction != ActionAllow {
		t.Errorf("expected default action allow, got %q", cfg.DefaultAction)
	}
	if cfg.SyncInterval != 60*time.Second {
		t.Errorf("expected 60s sync interval, got %v", cfg.SyncInterval)
	}
}

func TestNewEngine(t *testing.T) {
	cfg := DefaultConfig()
	e := NewEngine(cfg, nil)
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	if e.RuleCount() != 0 {
		t.Error("new engine should have zero rules")
	}
}

func TestAddRule(t *testing.T) {
	e := NewEngine(DefaultConfig(), nil)

	rule := Rule{
		ID:      "r1",
		Name:    "Block malware",
		Action:  ActionBlock,
		Enabled: true,
		Conditions: map[string]string{
			"dest_domain": "malware.example.com",
		},
	}
	err := e.AddRule(rule)
	if err != nil {
		t.Fatalf("AddRule returned error: %v", err)
	}
	if e.RuleCount() != 1 {
		t.Errorf("expected 1 rule, got %d", e.RuleCount())
	}
}

func TestAddRuleEmptyID(t *testing.T) {
	e := NewEngine(DefaultConfig(), nil)
	err := e.AddRule(Rule{Name: "no id"})
	if err == nil {
		t.Error("expected error for empty rule ID")
	}
}

func TestAddRuleDuplicate(t *testing.T) {
	e := NewEngine(DefaultConfig(), nil)
	rule := Rule{ID: "r1", Name: "test", Enabled: true}
	_ = e.AddRule(rule)
	err := e.AddRule(rule)
	if err == nil {
		t.Error("expected error for duplicate rule ID")
	}
}

func TestRemoveRule(t *testing.T) {
	e := NewEngine(DefaultConfig(), nil)
	_ = e.AddRule(Rule{ID: "r1", Name: "test", Enabled: true})

	err := e.RemoveRule("r1")
	if err != nil {
		t.Fatalf("RemoveRule returned error: %v", err)
	}
	if e.RuleCount() != 0 {
		t.Error("expected 0 rules after removal")
	}
}

func TestRemoveRuleNotFound(t *testing.T) {
	e := NewEngine(DefaultConfig(), nil)
	err := e.RemoveRule("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent rule")
	}
}

func TestEvaluateMatchingRule(t *testing.T) {
	e := NewEngine(DefaultConfig(), nil)
	_ = e.AddRule(Rule{
		ID:      "block-malware",
		Name:    "Block malware domain",
		Action:  ActionBlock,
		Enabled: true,
		Conditions: map[string]string{
			"dest_domain": "evil.com",
		},
	})

	req := Request{
		DestDomain: "evil.com",
		Protocol:   "https",
	}

	decision := e.Evaluate(context.Background(), req)
	if decision.Action != ActionBlock {
		t.Errorf("expected block action, got %q", decision.Action)
	}
	if decision.RuleID != "block-malware" {
		t.Errorf("expected rule ID 'block-malware', got %q", decision.RuleID)
	}
}

func TestEvaluateNoMatchDefaultAction(t *testing.T) {
	e := NewEngine(DefaultConfig(), nil)
	_ = e.AddRule(Rule{
		ID:      "r1",
		Name:    "specific rule",
		Action:  ActionBlock,
		Enabled: true,
		Conditions: map[string]string{
			"dest_domain": "blocked.com",
		},
	})

	req := Request{DestDomain: "allowed.com"}
	decision := e.Evaluate(context.Background(), req)
	if decision.Action != ActionAllow {
		t.Errorf("expected default allow action, got %q", decision.Action)
	}
}

func TestEvaluateDisabledRule(t *testing.T) {
	e := NewEngine(DefaultConfig(), nil)
	_ = e.AddRule(Rule{
		ID:      "r1",
		Name:    "disabled rule",
		Action:  ActionBlock,
		Enabled: false,
		Conditions: map[string]string{
			"dest_domain": "evil.com",
		},
	})

	req := Request{DestDomain: "evil.com"}
	decision := e.Evaluate(context.Background(), req)
	if decision.Action != ActionAllow {
		t.Error("disabled rule should not match")
	}
}

func TestRiskClassification(t *testing.T) {
	e := NewEngine(DefaultConfig(), nil)
	_ = e.AddRule(Rule{
		ID:      "r-block",
		Name:    "block rule",
		Action:  ActionBlock,
		Enabled: true,
		Conditions: map[string]string{
			"dest_domain": "risky.com",
		},
	})

	req := Request{DestDomain: "risky.com"}
	decision := e.Evaluate(context.Background(), req)
	if decision.RiskScore < 0.8 {
		t.Errorf("block rule should have high risk score, got %f", decision.RiskScore)
	}
	if decision.RiskLevel != RiskHigh && decision.RiskLevel != RiskCritical {
		t.Errorf("expected high or critical risk level, got %q", decision.RiskLevel)
	}
}

func TestActions(t *testing.T) {
	if ActionAllow != "allow" {
		t.Error("ActionAllow should be 'allow'")
	}
	if ActionBlock != "block" {
		t.Error("ActionBlock should be 'block'")
	}
	if ActionAlert != "alert" {
		t.Error("ActionAlert should be 'alert'")
	}
	if ActionPrompt != "prompt" {
		t.Error("ActionPrompt should be 'prompt'")
	}
}
