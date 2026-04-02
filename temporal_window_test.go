package arbiter

import (
	"strings"
	"testing"
)

func TestCompileRejectsInvalidActiveWindow(t *testing.T) {
	_, err := Compile([]byte(`
rule Windowed {
	active_from 2026-02-01T00:00:00Z
	active_until 2026-01-01T00:00:00Z
	when { true }
	then Allow {}
}
`))
	if err == nil || !strings.Contains(err.Error(), "active_from must be earlier than active_until") {
		t.Fatalf("expected active window ordering error, got %v", err)
	}
}

func TestEvalRespectsActiveWindow(t *testing.T) {
	prog, err := Compile([]byte(`
rule Windowed {
	active_from 2026-01-10T00:00:00Z
	active_until 2026-01-20T00:00:00Z
	when { true }
	then Allow {}
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	before := DataFromMap(map[string]any{"__now": "2026-01-09T23:59:59Z"}, prog)
	matched, err := Eval(prog, before)
	if err != nil {
		t.Fatalf("Eval before: %v", err)
	}
	if len(matched) != 0 {
		t.Fatalf("expected inactive rule before active_from, got %+v", matched)
	}

	during := DataFromMap(map[string]any{"__now": "2026-01-15T00:00:00Z"}, prog)
	matched, err = Eval(prog, during)
	if err != nil {
		t.Fatalf("Eval during: %v", err)
	}
	if len(matched) != 1 || matched[0].Name != "Windowed" {
		t.Fatalf("expected active rule during window, got %+v", matched)
	}

	atUntil := DataFromMap(map[string]any{"__now": "2026-01-20T00:00:00Z"}, prog)
	matched, err = Eval(prog, atUntil)
	if err != nil {
		t.Fatalf("Eval at active_until: %v", err)
	}
	if len(matched) != 0 {
		t.Fatalf("expected active_until to be exclusive, got %+v", matched)
	}
}
