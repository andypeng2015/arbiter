package arbtest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunFileCoverage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "r.arb"), []byte(`rule Tested { when { score > 10 } then A {} }
rule Untested { when { score < 0 } then B {} }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "r.test.arb"), []byte(`test "hit tested" {
    given { score: 20 }
    expect rule Tested matched
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := RunFile(filepath.Join(dir, "r.test.arb"), Options{})
	if err != nil {
		t.Fatalf("RunFile: %v", err)
	}
	if res.Coverage.Total != 2 {
		t.Fatalf("Coverage.Total = %d, want 2", res.Coverage.Total)
	}
	if len(res.Coverage.Covered) != 1 || res.Coverage.Covered[0] != "Tested" {
		t.Fatalf("Coverage.Covered = %v, want [Tested]", res.Coverage.Covered)
	}
	if len(res.Coverage.Uncovered) != 1 || res.Coverage.Uncovered[0] != "Untested" {
		t.Fatalf("Coverage.Uncovered = %v, want [Untested]", res.Coverage.Uncovered)
	}
	if got := res.Coverage.Percent(); got != 50 {
		t.Fatalf("Coverage.Percent = %.0f, want 50", got)
	}
}

// A rule referenced only by a `not matched` expectation still counts as covered.
func TestRunFileCoverageCountsNegativeExpectations(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "r.arb"), []byte(`rule R { when { score > 10 } then A {} }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "r.test.arb"), []byte(`test "miss" {
    given { score: 1 }
    expect rule R not matched
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RunFile(filepath.Join(dir, "r.test.arb"), Options{})
	if err != nil {
		t.Fatalf("RunFile: %v", err)
	}
	if len(res.Coverage.Uncovered) != 0 {
		t.Fatalf("a negatively-tested rule should be covered; uncovered = %v", res.Coverage.Uncovered)
	}
}
