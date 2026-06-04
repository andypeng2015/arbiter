package main

import (
	"strings"
	"testing"
)

func TestCompletionBash(t *testing.T) {
	out := captureStdout(t, func() {
		if err := runCompletion([]string{"bash"}); err != nil {
			t.Fatalf("completion bash: %v", err)
		}
	})
	if !strings.Contains(out, "complete -F _arbiter arbiter") {
		t.Fatalf("bash completion should register the function: %q", out)
	}
	if !strings.Contains(out, "check") || !strings.Contains(out, "eval") {
		t.Fatalf("completion should list subcommands: %q", out)
	}
}

func TestCompletionShells(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish"} {
		out := captureStdout(t, func() {
			if err := runCompletion([]string{sh}); err != nil {
				t.Fatalf("completion %s: %v", sh, err)
			}
		})
		if out == "" {
			t.Fatalf("completion %s produced no script", sh)
		}
	}
}

func TestCompletionUnknownShell(t *testing.T) {
	if err := runCompletion([]string{"tcsh"}); err == nil {
		t.Fatal("unknown shell should be an error")
	}
}
