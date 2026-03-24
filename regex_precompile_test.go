package arbiter

import (
	"strings"
	"testing"
)

// TestRegexInvalidLiteralIsCompileError verifies that an invalid literal regex
// pattern causes a compile-time error rather than a silent runtime failure.
func TestRegexInvalidLiteralIsCompileError(t *testing.T) {
	src := `rule T { when { name matches "([" } then A {} }`
	_, err := Compile([]byte(src))
	if err == nil {
		t.Fatal("expected compile error for invalid regex literal, got nil")
	}
	if !strings.Contains(err.Error(), "([") && !strings.Contains(err.Error(), "regex") && !strings.Contains(err.Error(), "regexp") {
		t.Logf("compile error: %v", err)
		// Error message is acceptable as long as it's a compile error
	}
}

// TestRegexValidLiteralMatchesAtRuntime verifies that a valid literal regex
// pattern compiles successfully and evaluates correctly.
func TestRegexValidLiteralMatchesAtRuntime(t *testing.T) {
	src := `rule T { when { name matches "^[A-Z].*$" } then A {} }`
	if !evalRule(t, src, map[string]any{"name": "Alice"}) {
		t.Error("expected match for 'Alice' against '^[A-Z].*$'")
	}
	if evalRule(t, src, map[string]any{"name": "alice"}) {
		t.Error("expected no match for 'alice' against '^[A-Z].*$'")
	}
}

// TestRegexPrecompiledUsedAtEval verifies that a valid literal pattern is
// stored in the compiled ruleset's Regexes map (pre-compiled at compile time).
func TestRegexPrecompiledUsedAtEval(t *testing.T) {
	src := `rule T { when { code matches "^[A-Z]{3}$" } then A {} }`
	prog, err := Compile([]byte(src))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	rs := prog.Ruleset
	if rs.Regexes == nil || len(rs.Regexes) == 0 {
		t.Fatal("expected at least one pre-compiled regex in CompiledRuleset.Regexes")
	}
	// Verify the regex is actually correct (compiles the right pattern)
	for _, re := range rs.Regexes {
		if !re.MatchString("ABC") {
			t.Error("pre-compiled regex should match 'ABC'")
		}
		if re.MatchString("ab") {
			t.Error("pre-compiled regex should not match 'ab'")
		}
	}
}

// TestRegexDynamicPatternWorksAtRuntime verifies that a dynamic (variable)
// pattern that is not a literal string still works correctly via the lazy
// compile path.
func TestRegexDynamicPatternWorksAtRuntime(t *testing.T) {
	src := `rule T { when { name matches pattern } then A {} }`
	if !evalRule(t, src, map[string]any{"name": "Alice", "pattern": "^[A-Z].*$"}) {
		t.Error("expected match for dynamic pattern")
	}
	if evalRule(t, src, map[string]any{"name": "alice", "pattern": "^[A-Z].*$"}) {
		t.Error("expected no match for dynamic pattern")
	}
}

// TestRegexMultipleLiteralsPrecompiled verifies that multiple distinct literal
// patterns in one ruleset are each pre-compiled.
func TestRegexMultipleLiteralsPrecompiled(t *testing.T) {
	src := `
rule T1 { when { code matches "^[A-Z]{3}$" } then A {} }
rule T2 { when { zip matches "^[0-9]{5}$" } then B {} }
`
	prog, err := Compile([]byte(src))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	rs := prog.Ruleset
	if len(rs.Regexes) < 2 {
		t.Fatalf("expected 2 pre-compiled regexes, got %d", len(rs.Regexes))
	}
}
