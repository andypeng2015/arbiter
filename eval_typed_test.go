package arbiter

import "testing"

type evalTestFacts struct {
	Count  int    `arb:"count"`
	Status string `arb:"status"`
}

func TestEvalTyped_BasicRule(t *testing.T) {
	src := []byte(`
rule HighCount priority 10 {
    when {
        count > 5
    }
    then Flag {
        action: "flag",
    }
}

rule LowCount priority 5 {
    when {
        count <= 5
    }
    then Pass {
        action: "pass",
    }
}
`)
	rs, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	matched, err := EvalTyped(rs, evalTestFacts{Count: 10, Status: "active"})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(matched) == 0 {
		t.Fatal("expected at least one match")
	}
	if matched[0].Action != "Flag" {
		t.Errorf("action = %q, want %q", matched[0].Action, "Flag")
	}
}
