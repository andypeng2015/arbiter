package arbiter

import (
	"testing"
)

// These tests verify that deprecated v1.0 functions produce correct results
// that are consistent with the new *Program-based API.

const paritySource = `
rule HighValue priority 1 {
    when {
        order.amount > 100
    }
    then ApplyDiscount {
        type: "percentage",
        amount: 10,
    }
}
`

func TestDeprecatedCompileFullParity(t *testing.T) {
	src := []byte(paritySource)

	// New API
	prog, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// Deprecated API
	cr, err := CompileFull(src)
	if err != nil {
		t.Fatalf("CompileFull: %v", err)
	}

	if cr.Ruleset == nil {
		t.Fatal("CompileFull: nil Ruleset")
	}
	if len(cr.Ruleset.Rules) != len(prog.Ruleset.Rules) {
		t.Fatalf("CompileFull: rule count mismatch: got %d, want %d",
			len(cr.Ruleset.Rules), len(prog.Ruleset.Rules))
	}
}

func TestDeprecatedCompileFullParsedParity(t *testing.T) {
	src := []byte(paritySource)

	parsed, err := ParseSource(src)
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	// New API
	prog, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// Deprecated API
	cr, err := CompileFullParsed(parsed)
	if err != nil {
		t.Fatalf("CompileFullParsed: %v", err)
	}

	if cr.Ruleset == nil {
		t.Fatal("CompileFullParsed: nil Ruleset")
	}
	if len(cr.Ruleset.Rules) != len(prog.Ruleset.Rules) {
		t.Fatalf("CompileFullParsed: rule count mismatch: got %d, want %d",
			len(cr.Ruleset.Rules), len(prog.Ruleset.Rules))
	}
}

func TestDeprecatedCompileParsedParity(t *testing.T) {
	src := []byte(paritySource)

	parsed, err := ParseSource(src)
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	// New API
	prog, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// Deprecated API
	rs, err := CompileParsed(parsed)
	if err != nil {
		t.Fatalf("CompileParsed: %v", err)
	}

	if rs == nil {
		t.Fatal("CompileParsed: nil ruleset")
	}
	if len(rs.Rules) != len(prog.Ruleset.Rules) {
		t.Fatalf("CompileParsed: rule count mismatch: got %d, want %d",
			len(rs.Rules), len(prog.Ruleset.Rules))
	}
}

func TestDeprecatedCompileJSONParity(t *testing.T) {
	// Arishem-format JSON condition and action.
	condJSON := `{"Operator":"==","Lhs":{"VarExpr":"x"},"Rhs":{"Const":{"NumConst":1}}}`
	actJSON := `{"ActionName":"ApplyDiscount"}`

	rs, err := CompileJSON(condJSON, actJSON)
	if err != nil {
		t.Fatalf("CompileJSON: %v", err)
	}
	if rs == nil {
		t.Fatal("CompileJSON: nil ruleset")
	}
	if len(rs.Rules) != 1 {
		t.Fatalf("CompileJSON: expected 1 rule, got %d", len(rs.Rules))
	}
}

func TestDeprecatedCompileJSONRulesParity(t *testing.T) {
	rules := []JSONRule{
		{
			Name:      "rule0",
			Priority:  0,
			Condition: `{"Operator":"==","Lhs":{"VarExpr":"x"},"Rhs":{"Const":{"NumConst":1}}}`,
			Action:    `{"ActionName":"ApplyDiscount"}`,
		},
		{
			Name:      "rule1",
			Priority:  1,
			Condition: `{"Operator":"==","Lhs":{"VarExpr":"y"},"Rhs":{"Const":{"NumConst":2}}}`,
			Action:    `{"ActionName":"DoSomething"}`,
		},
	}

	rs, err := CompileJSONRules(rules)
	if err != nil {
		t.Fatalf("CompileJSONRules: %v", err)
	}
	if rs == nil {
		t.Fatal("CompileJSONRules: nil ruleset")
	}
	if len(rs.Rules) != 2 {
		t.Fatalf("CompileJSONRules: expected 2 rules, got %d", len(rs.Rules))
	}
}

func TestDeprecatedCompileStrategiesParity(t *testing.T) {
	src := []byte(`
outcome CheckoutPath {
	target: string
}

strategy CheckoutRouting returns CheckoutPath {
	when {
		user.country == "US"
	} then Domestic {
		target: "domestic",
	}

	else Global {
		target: "global",
	}
}
`)
	// Deprecated API
	strategies, err := CompileStrategies(src)
	if err != nil {
		t.Fatalf("CompileStrategies: %v", err)
	}

	// New API
	prog, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if strategies == nil {
		t.Fatal("CompileStrategies: nil strategies")
	}
	if prog.Strategies == nil {
		t.Fatal("Compile: nil strategies")
	}
	if !strategies.Has("CheckoutRouting") {
		t.Fatal("CompileStrategies: missing CheckoutRouting")
	}
	if !prog.Strategies.Has("CheckoutRouting") {
		t.Fatal("Compile: missing CheckoutRouting")
	}
}

func TestDeprecatedCompileStrategiesParsedParity(t *testing.T) {
	src := []byte(`
outcome Path { target: string }
strategy Route returns Path {
	when { user.vip == true } then VIP { target: "vip" }
	else Standard { target: "std" }
}
`)
	parsed, err := ParseSource(src)
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}

	// Deprecated API
	strategies, err := CompileStrategiesParsed(parsed)
	if err != nil {
		t.Fatalf("CompileStrategiesParsed: %v", err)
	}
	if strategies == nil {
		t.Fatal("CompileStrategiesParsed: nil strategies")
	}
	if !strategies.Has("Route") {
		t.Fatal("CompileStrategiesParsed: missing Route")
	}
}
