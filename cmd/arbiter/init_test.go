package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitScaffold(t *testing.T) {
	dir := t.TempDir()
	if err := runInit([]string{dir}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rules.arb")); err != nil {
		t.Fatalf("rules.arb not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rules.test.arb")); err != nil {
		t.Fatalf("rules.test.arb not created: %v", err)
	}
	// The scaffold must be clean: check --strict passes (no warnings).
	if err := check(filepath.Join(dir, "rules.arb"), true); err != nil {
		t.Fatalf("scaffold should pass check --strict: %v", err)
	}
	// And its tests must pass.
	if err := runTest([]string{filepath.Join(dir, "rules.test.arb")}); err != nil {
		t.Fatalf("scaffold tests should pass: %v", err)
	}
}

func TestInitModuleScaffold(t *testing.T) {
	dir := t.TempDir()
	if err := runInit([]string{dir, "--module"}); err != nil {
		t.Fatalf("init --module: %v", err)
	}
	for _, f := range []string{"arbiter.toml", "lib/limits.arb", "main.arb"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("%s not created: %v", f, err)
		}
	}
	// The modular scaffold must compile with imports resolved and be warning-clean.
	if err := check(filepath.Join(dir, "main.arb"), true); err != nil {
		t.Fatalf("modular scaffold should pass check --strict: %v", err)
	}
}

func TestInitRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := runInit([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if err := runInit([]string{dir}); err == nil {
		t.Fatal("init should refuse to overwrite existing files")
	}
}
