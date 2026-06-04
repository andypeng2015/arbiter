package arbiter

import "testing"

func TestWorstCaseCostQuantifierExceedsSimple(t *testing.T) {
	simple, err := Compile([]byte(`rule R { when { score > 1 } then A {} }`))
	if err != nil {
		t.Fatal(err)
	}
	loopy, err := Compile([]byte(`rule R { when { any v in items { v > 1 } } then A {} }`))
	if err != nil {
		t.Fatal(err)
	}

	sc, _ := simple.Ruleset.WorstCaseCost(0)
	lc, lname := loopy.Ruleset.WorstCaseCost(0)

	if lc <= sc {
		t.Fatalf("quantifier worst-case cost %d should exceed simple cost %d", lc, sc)
	}
	// A loop body must reflect the loop bound (default 100), not a flat count.
	if lc < 100 {
		t.Fatalf("quantifier worst-case %d should reflect the loop bound", lc)
	}
	if lname != "R" {
		t.Fatalf("worst-case rule name = %q, want R", lname)
	}
}

func TestWorstCaseCostDeterministic(t *testing.T) {
	prog, err := Compile([]byte(`rule R { when { any v in items { v > 1 } } then A {} }`))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := prog.Ruleset.WorstCaseCost(0)
	b, _ := prog.Ruleset.WorstCaseCost(0)
	if a != b {
		t.Fatalf("estimate not deterministic: %d vs %d", a, b)
	}
}
