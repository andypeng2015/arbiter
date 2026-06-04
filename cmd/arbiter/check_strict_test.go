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
