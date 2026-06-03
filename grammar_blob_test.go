package arbiter

import (
	"bytes"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammargen"
)

// TestGrammarBinIsCurrent fails if the embedded grammar.bin has drifted from
// ArbiterGrammar(). If it fails, run: go generate ./...
func TestGrammarBinIsCurrent(t *testing.T) {
	_, fresh, err := grammargen.GenerateLanguageAndBlob(ArbiterGrammar())
	if err != nil {
		t.Fatalf("GenerateLanguageAndBlob: %v", err)
	}
	if !bytes.Equal(fresh, grammarBlob) {
		t.Fatalf("grammar.bin is stale (embedded %d bytes, regenerated %d bytes) — run `go generate ./...`",
			len(grammarBlob), len(fresh))
	}
}

// TestLanguageLoadsFromEmbeddedBlob confirms the runtime uses the embedded,
// pre-generated parse table and that it parses successfully.
func TestLanguageLoadsFromEmbeddedBlob(t *testing.T) {
	if len(grammarBlob) == 0 {
		t.Fatal("grammar.bin embed is empty")
	}
	lang, err := gotreesitter.LoadLanguage(grammarBlob)
	if err != nil {
		t.Fatalf("LoadLanguage(embedded grammar.bin): %v", err)
	}
	if _, err := parseTreeWithLanguage([]byte(`rule R { when { x > 1 } then A {} }`), lang); err != nil {
		t.Fatalf("parse with embedded language: %v", err)
	}
}
