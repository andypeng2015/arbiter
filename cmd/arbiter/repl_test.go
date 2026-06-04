package main

import (
	"bytes"
	"strings"
	"testing"

	arbiter "m31labs.dev/arbiter"
)

func TestReplEvaluatesContexts(t *testing.T) {
	prog, err := arbiter.Compile([]byte(`rule Big { when { score > 10 } then Flag { level: "high" } }`))
	if err != nil {
		t.Fatal(err)
	}
	in := strings.NewReader(`{"score": 20}` + "\n" + `{"score": 5}` + "\nexit\n")
	var out bytes.Buffer
	if err := repl(prog, in, &out); err != nil {
		t.Fatalf("repl: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "Big") {
		t.Fatalf("first context (score 20) should match rule Big: %q", s)
	}
	if !strings.Contains(s, "no rules matched") {
		t.Fatalf("second context (score 5) should match nothing: %q", s)
	}
}

func TestReplReportsBadJSON(t *testing.T) {
	prog, err := arbiter.Compile([]byte(`rule R { when { score > 1 } then A {} }`))
	if err != nil {
		t.Fatal(err)
	}
	in := strings.NewReader("not json\nexit\n")
	var out bytes.Buffer
	if err := repl(prog, in, &out); err != nil {
		t.Fatalf("repl should not error on bad input line: %v", err)
	}
	if !strings.Contains(out.String(), "error") {
		t.Fatalf("bad JSON should report an error line: %q", out.String())
	}
}
