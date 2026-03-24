package observability

import (
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	if ParseLevel("debug") != slog.LevelDebug {
		t.Fatal("debug")
	}
	if ParseLevel("warn") != slog.LevelWarn {
		t.Fatal("warn")
	}
	if ParseLevel("error") != slog.LevelError {
		t.Fatal("error")
	}
	if ParseLevel("info") != slog.LevelInfo {
		t.Fatal("info")
	}
	if ParseLevel("") != slog.LevelInfo {
		t.Fatal("empty default")
	}
}
