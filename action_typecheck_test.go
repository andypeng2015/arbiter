package arbiter

import (
	"strings"
	"testing"
)

// TestRuleActionUnknownFieldRejected verifies that a rule action param with a
// field name not declared in the matching outcome schema is rejected at compile
// time.
func TestRuleActionUnknownFieldRejected(t *testing.T) {
	_, err := Compile([]byte(`
outcome Profile {
	x: string
}

rule R {
	when { true }
	then Profile {
		x: "ok",
		bogus: 42,
	}
}
`))
	if err == nil {
		t.Fatal("expected compile error for unknown field, got nil")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("expected error to mention field %q, got: %v", "bogus", err)
	}
}

// TestRuleActionTypeMismatchRejected verifies that a rule action param whose
// value type does not match the declared outcome schema field type is rejected.
func TestRuleActionTypeMismatchRejected(t *testing.T) {
	_, err := Compile([]byte(`
outcome Profile {
	x: string
}

rule R {
	when { true }
	then Profile {
		x: 123,
	}
}
`))
	if err == nil {
		t.Fatal("expected compile error for type mismatch, got nil")
	}
	if !strings.Contains(err.Error(), `"x"`) {
		t.Fatalf("expected error to mention field %q, got: %v", "x", err)
	}
}

// TestRuleActionMissingRequiredFieldRejected verifies that omitting a required
// outcome schema field in a rule action is rejected.
func TestRuleActionMissingRequiredFieldRejected(t *testing.T) {
	_, err := Compile([]byte(`
outcome Profile {
	x: string
	y: string
}

rule R {
	when { true }
	then Profile {
		x: "ok",
	}
}
`))
	if err == nil {
		t.Fatal("expected compile error for missing required field, got nil")
	}
	if !strings.Contains(err.Error(), "y") {
		t.Fatalf("expected error to mention missing field %q, got: %v", "y", err)
	}
}

// TestRuleActionUnknownActionNamePasses verifies that a rule action whose name
// does not match any declared outcome schema passes through silently (backward
// compatibility).
func TestRuleActionUnknownActionNamePasses(t *testing.T) {
	_, err := Compile([]byte(`
rule R {
	when { true }
	then Whatever {
		anything: 42,
	}
}
`))
	if err != nil {
		t.Fatalf("expected no compile error for unknown action name, got: %v", err)
	}
}

// TestRuleActionOptionalFieldMissingIsOK verifies that an optional outcome
// schema field may be omitted from a rule action without error.
func TestRuleActionOptionalFieldMissingIsOK(t *testing.T) {
	_, err := Compile([]byte(`
outcome Profile {
	x: string
	reason?: string
}

rule R {
	when { true }
	then Profile {
		x: "ok",
	}
}
`))
	if err != nil {
		t.Fatalf("expected no compile error when optional field is omitted, got: %v", err)
	}
}

// TestRuleFallbackUnknownFieldRejected verifies the same validation applies to
// the fallback action of a rule.
func TestRuleFallbackUnknownFieldRejected(t *testing.T) {
	_, err := Compile([]byte(`
outcome Profile {
	x: string
}

rule R {
	when { true }
	then Profile { x: "ok" }
	otherwise Profile { x: "ok", bogus: 42 }
}
`))
	if err == nil {
		t.Fatal("expected compile error for unknown fallback field, got nil")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("expected error to mention field %q, got: %v", "bogus", err)
	}
}
