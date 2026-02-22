package pac

import "testing"

func TestNewEngine(t *testing.T) {
	e := NewEngine()
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	if e.RuleCount() != 0 {
		t.Error("new engine should have zero rules")
	}
}

func TestAddRule(t *testing.T) {
	e := NewEngine()
	err := e.AddRule("*.example.com", ActionProxy, 10)
	if err != nil {
		t.Fatalf("AddRule returned error: %v", err)
	}
	if e.RuleCount() != 1 {
		t.Errorf("expected 1 rule, got %d", e.RuleCount())
	}
}

func TestAddRuleEmptyPattern(t *testing.T) {
	e := NewEngine()
	err := e.AddRule("", ActionProxy, 10)
	if err == nil {
		t.Error("expected error for empty pattern")
	}
}

func TestAddRuleNegativePriority(t *testing.T) {
	e := NewEngine()
	err := e.AddRule("*.test.com", ActionProxy, -1)
	if err == nil {
		t.Error("expected error for negative priority")
	}
}

func TestEvaluateExactMatch(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("example.com", ActionBlock, 10)

	action := e.Evaluate("example.com")
	if action != ActionBlock {
		t.Errorf("expected ActionBlock, got %d", action)
	}
}

func TestEvaluateWildcardMatch(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("*.example.com", ActionTunnel, 10)

	action := e.Evaluate("app.example.com")
	if action != ActionTunnel {
		t.Errorf("expected ActionTunnel, got %d", action)
	}
}

func TestEvaluateWildcardRootMatch(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("*.example.com", ActionTunnel, 10)

	action := e.Evaluate("example.com")
	if action != ActionTunnel {
		t.Errorf("expected ActionTunnel for root domain, got %d", action)
	}
}

func TestEvaluateNoMatch(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("specific.com", ActionBlock, 10)

	action := e.Evaluate("other.com")
	if action != ActionProxy {
		t.Errorf("expected default ActionProxy, got %d", action)
	}
}

func TestEvaluateEmptyHostname(t *testing.T) {
	e := NewEngine()
	action := e.Evaluate("")
	if action != ActionDirect {
		t.Errorf("empty hostname should return ActionDirect, got %d", action)
	}
}

func TestEvaluateCatchAll(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("*", ActionDirect, 0)

	action := e.Evaluate("anything.com")
	if action != ActionDirect {
		t.Errorf("catch-all should return ActionDirect, got %d", action)
	}
}

func TestPriorityOrdering(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("*.example.com", ActionProxy, 5)
	_ = e.AddRule("blocked.example.com", ActionBlock, 10)

	// Higher priority rule should match first
	action := e.Evaluate("blocked.example.com")
	if action != ActionBlock {
		t.Errorf("higher priority rule should win, got %d", action)
	}
}

func TestClearRules(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("*.example.com", ActionProxy, 10)
	_ = e.AddRule("*.test.com", ActionBlock, 5)

	e.ClearRules()
	if e.RuleCount() != 0 {
		t.Errorf("expected 0 rules after clear, got %d", e.RuleCount())
	}
}

func TestCaseInsensitiveEval(t *testing.T) {
	e := NewEngine()
	_ = e.AddRule("Example.COM", ActionBlock, 10)

	action := e.Evaluate("example.com")
	if action != ActionBlock {
		t.Error("evaluation should be case-insensitive")
	}
}

func TestActionConstants(t *testing.T) {
	if ActionDirect != 0 {
		t.Error("ActionDirect should be 0")
	}
	if ActionProxy != 1 {
		t.Error("ActionProxy should be 1")
	}
	if ActionBlock != 2 {
		t.Error("ActionBlock should be 2")
	}
	if ActionTunnel != 3 {
		t.Error("ActionTunnel should be 3")
	}
}
