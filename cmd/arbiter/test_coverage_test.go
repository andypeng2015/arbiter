package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTestCoverageThresholdGate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "r.arb"), []byte("rule Tested { when { score > 10 } then A {} }\nrule Untested { when { score < 0 } then B {} }"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "r.test.arb"), []byte(`test "hit" { given { score: 20 } expect rule Tested matched }`), 0o644); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(dir, "r.test.arb")

	// 50% coverage: a threshold of 80 must fail.
	if err := runTest([]string{testFile, "--coverage", "--threshold", "80"}); err == nil {
		t.Fatal("coverage 50%% should fail a --threshold 80")
	}
	// A threshold of 50 is met.
	if err := runTest([]string{testFile, "--coverage", "--threshold", "50"}); err != nil {
		t.Fatalf("coverage 50%% should satisfy --threshold 50: %v", err)
	}
}
