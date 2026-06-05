package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvalAppliesSegmentGating(t *testing.T) {
	dir := t.TempDir()
	arbPath := filepath.Join(dir, "seg.arb")
	src := `input { user: { age: number } }
segment Adults { user.age >= 18 }
outcome Ok { v: string }
rule R { when segment Adults { user.age < 200 } then Ok { v: "y" } }
`
	if err := os.WriteFile(arbPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// age 10 fails the Adults segment, so the rule must not match — `arbiter
	// eval` must reflect the same governed semantics as the server, the SDK,
	// and `arbiter test` (which all use EvalGoverned), not the raw rule layer.
	out := captureStdout(t, func() {
		if err := runEval([]string{arbPath, "--data", `{"user":{"age":10}}`}); err != nil {
			t.Fatalf("eval: %v", err)
		}
	})
	if !strings.Contains(out, "no rules matched") {
		t.Fatalf("eval must apply segment gating (age 10 < Adults 18); got: %q", out)
	}
}

func TestEvalExplainPrintsArbitrace(t *testing.T) {
	dir := t.TempDir()
	arbPath := filepath.Join(dir, "r.arb")
	if err := os.WriteFile(arbPath, []byte("rule Big { when { score > 10 } then A {} }"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := runEval([]string{arbPath, "--data", `{"score":20}`, "--explain"}); err != nil {
			t.Fatalf("eval --explain: %v", err)
		}
	})
	if !strings.Contains(out, "arbitrace") {
		t.Fatalf("--explain should print an arbitrace section: %q", out)
	}
	if !strings.Contains(out, "Big") {
		t.Fatalf("--explain should show the matched rule Big: %q", out)
	}
}
