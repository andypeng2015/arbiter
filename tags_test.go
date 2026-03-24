package arbiter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvalWithTagsFiltersRules(t *testing.T) {
	prog, err := Compile([]byte(`
tag "fraud"
tag "realtime"
tag "batch"

rule FraudRealtime tag "fraud" tag "realtime" {
	when { true }
	then A {}
}

rule FraudBatch tags "fraud,batch" {
	when { true }
	then B {}
}

rule Untagged {
	when { true }
	then C {}
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	dc := DataFromMap(map[string]any{}, prog)
	all, err := Eval(prog, dc)
	if err != nil {
		t.Fatalf("eval all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("all matches = %d, want 3", len(all))
	}

	fraud, err := Eval(prog, dc, WithTags("fraud"))
	if err != nil {
		t.Fatalf("eval fraud: %v", err)
	}
	if len(fraud) != 2 {
		t.Fatalf("fraud matches = %d, want 2", len(fraud))
	}

	realtime, err := Eval(prog, dc, WithTags("fraud", "realtime"))
	if err != nil {
		t.Fatalf("eval fraud+realtime: %v", err)
	}
	if len(realtime) != 1 || realtime[0].Name != "FraudRealtime" {
		t.Fatalf("fraud+realtime matches = %+v, want FraudRealtime only", realtime)
	}

	none, err := Eval(prog, dc, WithTags("missing"))
	if err != nil {
		t.Fatalf("eval missing: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("missing matches = %+v, want none", none)
	}
}

func TestCompileRejectsUnknownTagWithSuggestion(t *testing.T) {
	_, err := Compile([]byte(`
tag "fraud"

rule Check tag "frad" {
	when { true }
	then A {}
}
`))
	if err == nil {
		t.Fatal("expected unknown tag error")
	}
	if !strings.Contains(err.Error(), `unknown tag "frad"`) {
		t.Fatalf("expected unknown tag error, got: %v", err)
	}
	if !strings.Contains(err.Error(), `did you mean "fraud"`) {
		t.Fatalf("expected suggestion, got: %v", err)
	}
}

func TestCompileWarnsOnUnusedTag(t *testing.T) {
	prog, err := Compile([]byte(`
tag "fraud"
tag "unused"

rule Check tag "fraud" {
	when { true }
	then A {}
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	found := false
	for _, warning := range prog.Warnings {
		if strings.Contains(warning.Message, `tag "unused" declared but not used`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unused tag warning, got %+v", prog.Warnings)
	}
}

func TestModuleImportsTagDeclarations(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "arbiter.toml"), []byte(`[project]
name = "tags"
version = "1.0.0"
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "base.arb"), []byte(`
tag "fraud"

rule Base tag "fraud" {
	when { true }
	then A {}
}
`), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	main := filepath.Join(dir, "main.arb")
	if err := os.WriteFile(main, []byte(`
import "base"

rule Main tag "fraud" {
	when { true }
	then B {}
}
`), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	prog, err := CompileFile(main)
	if err != nil {
		t.Fatalf("CompileFile: %v", err)
	}
	dc := DataFromMap(map[string]any{}, prog)
	matched, err := Eval(prog, dc, WithTags("fraud"))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(matched) != 2 {
		t.Fatalf("fraud matches = %d, want 2", len(matched))
	}
}
