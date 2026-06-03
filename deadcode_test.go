package arbiter

import (
	"strings"
	"testing"
)

func warningsContain(p *Program, sub string) bool {
	for _, w := range p.Warnings {
		if strings.Contains(w.Message, sub) {
			return true
		}
	}
	return false
}

func TestDeadCodeRuleAlwaysFalse(t *testing.T) {
	p, err := Compile([]byte(`rule R { when { 1 > 2 } then A {} }`))
	if err != nil {
		t.Fatal(err)
	}
	if !warningsContain(p, "never match") {
		t.Fatalf("want an always-false rule warning, got %v", p.Warnings)
	}
}

func TestDeadCodeStrategyUnreachableCandidate(t *testing.T) {
	p, err := Compile([]byte(`outcome O { x: string }
strategy S returns O {
    when { true } then A { x: "a" }
    when { score > 5 } then B { x: "b" }
    else C { x: "c" }
}`))
	if err != nil {
		t.Fatal(err)
	}
	if !warningsContain(p, "unreachable") {
		t.Fatalf("want an unreachable-candidate warning, got %v", p.Warnings)
	}
}

func TestDeadCodeNoFalsePositives(t *testing.T) {
	p, err := Compile([]byte(`outcome O { x: string }
rule Live { when { score > 18 } then A {} }
strategy S returns O {
    when { score > 5 } then A { x: "a" }
    else B { x: "b" }
}`))
	if err != nil {
		t.Fatal(err)
	}
	if warningsContain(p, "never match") || warningsContain(p, "unreachable") {
		t.Fatalf("unexpected dead-code warning on live logic: %v", p.Warnings)
	}
}
