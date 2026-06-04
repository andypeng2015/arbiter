package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
