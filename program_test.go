package arbiter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileReturnsProgram(t *testing.T) {
	src := []byte(`rule R { when { x > 1 } then A {} }`)
	prog, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	if prog.Ruleset == nil {
		t.Fatal("expected non-nil Ruleset")
	}
	if prog.IR == nil {
		t.Fatal("expected non-nil IR")
	}
}

func TestCompileBytesRejectsImportWithoutResolver(t *testing.T) {
	src := []byte(`import "fraud/scoring"
rule R { when { true } then A {} }`)
	_, err := Compile(src)
	if err == nil {
		t.Fatal("expected error for import without resolver")
	}
	if !strings.Contains(err.Error(), "import requires") {
		t.Fatalf("expected 'import requires' error, got: %v", err)
	}
}

func TestCompileFileReturnsProgram(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rules.arb"), []byte(`rule R { when { x > 1 } then A {} }`), 0644); err != nil {
		t.Fatal(err)
	}
	prog, err := CompileFile(filepath.Join(dir, "rules.arb"))
	if err != nil {
		t.Fatal(err)
	}
	if prog.Ruleset == nil {
		t.Fatal("expected non-nil Ruleset")
	}
}

func TestProgramToCompileResultRoundTrip(t *testing.T) {
	src := []byte(`
segment vip { user.tier == "gold" }
rule VIPOffer {
	when segment vip { user.cart > 50 }
	then Offer { discount: 20 }
}
`)
	prog, err := Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	cr := prog.toCompileResult()
	if cr == nil {
		t.Fatal("expected non-nil CompileResult")
	}
	if cr.Ruleset != prog.Ruleset {
		t.Fatal("Ruleset should be the same pointer")
	}
	if cr.Segments != prog.Segments {
		t.Fatal("Segments should be the same pointer")
	}
	if cr.Program != prog.IR {
		t.Fatal("Program should match IR")
	}
}
