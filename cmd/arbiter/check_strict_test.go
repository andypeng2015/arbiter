package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckStrictFailsOnDeadCode(t *testing.T) {
	dir := t.TempDir()
	arbPath := filepath.Join(dir, "r.arb")
	if err := os.WriteFile(arbPath, []byte("rule R { when { 1 > 2 } then A {} }"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Plain check tolerates warnings (still exits 0).
	if err := runCheck([]string{arbPath}); err != nil {
		t.Fatalf("plain check should pass despite a warning: %v", err)
	}
	// --strict turns dead-code warnings into a failure (CI gate).
	if err := runCheck([]string{arbPath, "--strict"}); err == nil {
		t.Fatal("check --strict should fail on a dead-code warning")
	}
}

func TestCheckReportsImportedFieldTypeError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "arbiter.toml"),
		[]byte("[project]\nname = \"t\"\nversion = \"0.1.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib", "order.arb"),
		[]byte("input { order: { amount: number } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.arb")
	if err := os.WriteFile(main, []byte(`import "lib/order"
rule R { when { order.amount == "notanumber" } then Flag {} }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// The imported field's type mismatch must surface: check must not swallow the
	// resolution-aware compile error and fall back to an import-blind pass.
	if err := runCheck([]string{main}); err == nil {
		t.Fatal("check must report the imported-field type mismatch; got nil (error swallowed?)")
	}
}

func TestCheckStrictPassesCleanProgram(t *testing.T) {
	dir := t.TempDir()
	arbPath := filepath.Join(dir, "r.arb")
	if err := os.WriteFile(arbPath, []byte("rule R { when { score > 1 } then A {} }"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCheck([]string{arbPath, "--strict"}); err != nil {
		t.Fatalf("check --strict should pass a clean program: %v", err)
	}
}
