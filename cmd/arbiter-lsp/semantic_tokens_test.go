package main

import (
	"os"
	"path/filepath"
	"testing"

	arbiter "github.com/odvcencio/arbiter"
)

func TestComputeSemanticTokens_ExpertAssert(t *testing.T) {
	source := []byte(`fact Alert { severity: string }
expert rule E {
	when { true }
	then assert Alert { severity: "high" }
}
`)
	full, err := arbiter.CompileFull(source)
	if err != nil {
		t.Fatal(err)
	}
	tokens := computeSemanticTokens(source, full.Program)

	// "Alert" in "assert Alert" should be type token (1).
	found := false
	for _, tok := range tokens {
		if tok.tokenType == 1 && tok.length == len("Alert") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected type token for 'Alert' in assert, got tokens: %+v", tokens)
	}
}

func TestComputeSemanticTokens_MemberAccess(t *testing.T) {
	source := []byte(`rule R {
	when { user.age > 18 }
	then Allow {}
}
`)
	full, err := arbiter.CompileFull(source)
	if err != nil {
		t.Fatal(err)
	}
	tokens := computeSemanticTokens(source, full.Program)

	// "age" in "user.age" should be property token (3).
	found := false
	for _, tok := range tokens {
		if tok.tokenType == 3 && tok.length == len("age") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected property token for 'age' in member access, got tokens: %+v", tokens)
	}
}

func TestComputeSemanticTokens_LookupTable(t *testing.T) {
	source := []byte(`table limits {
	score: number | verdict: string
	1 | "review"
}

rule R {
	when { true }
	then Allow {
		let row = lookup limits where score > 0
		result: row.verdict,
	}
}
`)
	full, err := arbiter.CompileFull(source)
	if err != nil {
		t.Fatal(err)
	}
	tokens := computeSemanticTokens(source, full.Program)

	// "limits" in "lookup limits" should be struct token (2).
	found := false
	for _, tok := range tokens {
		if tok.tokenType == 2 && tok.length == len("limits") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected struct token for 'limits' in lookup, got tokens: %+v", tokens)
	}
}

func TestComputeSemanticTokens_QualifiedNamespace(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.arb")
	scoringPath := filepath.Join(dir, "scoring.arb")

	mainContent := []byte(`import "scoring"
rule Derived {
	requires scoring.BaseRule
	when { true }
	then Allow {}
}
`)
	scoringContent := []byte(`rule BaseRule {
	when { true }
	then Allow {}
}
`)
	if err := os.WriteFile(mainPath, mainContent, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scoringPath, scoringContent, 0o644); err != nil {
		t.Fatal(err)
	}

	full, err := arbiter.CompileFullFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	tokens := computeSemanticTokens(mainContent, full.Program)

	// "scoring" in "scoring.BaseRule" should be namespace token (0).
	foundNS := false
	for _, tok := range tokens {
		if tok.tokenType == 0 && tok.length == len("scoring") {
			foundNS = true
			break
		}
	}
	if !foundNS {
		t.Errorf("expected namespace token for 'scoring' in qualified name, got tokens: %+v", tokens)
	}
}

func TestComputeSemanticTokens_DeltaEncoding(t *testing.T) {
	source := []byte(`fact Alert { severity: string }
expert rule E {
	when { true }
	then assert Alert { severity: "high" }
}
`)
	full, err := arbiter.CompileFull(source)
	if err != nil {
		t.Fatal(err)
	}
	tokens := computeSemanticTokens(source, full.Program)
	encoded := encodeSemanticTokens(tokens)

	// Every 5 ints = one token, so length must be multiple of 5.
	if len(encoded)%5 != 0 {
		t.Errorf("encoded tokens length %d is not a multiple of 5", len(encoded))
	}
	// Delta line must be non-negative.
	for i := 0; i < len(encoded); i += 5 {
		if encoded[i] < 0 {
			t.Errorf("negative deltaLine at token %d: %d", i/5, encoded[i])
		}
	}
}

func TestComputeSemanticTokens_MultiDotMemberAccess(t *testing.T) {
	source := []byte(`rule R {
	when { user.profile.age > 18 }
	then Allow {}
}
`)
	full, err := arbiter.CompileFull(source)
	if err != nil {
		t.Fatal(err)
	}
	tokens := computeSemanticTokens(source, full.Program)

	// Both "profile" and "age" should be property tokens (3).
	propCount := 0
	for _, tok := range tokens {
		if tok.tokenType == 3 {
			propCount++
		}
	}
	if propCount < 2 {
		t.Errorf("expected at least 2 property tokens for multi-dot member access, got %d; tokens: %+v", propCount, tokens)
	}
}

func TestComputeSemanticTokens_ExpertEmit(t *testing.T) {
	source := []byte(`outcome Result { status: string }
expert rule E {
	when { true }
	then emit Result { status: "ok" }
}
`)
	full, err := arbiter.CompileFull(source)
	if err != nil {
		t.Fatal(err)
	}
	tokens := computeSemanticTokens(source, full.Program)

	// "Result" in "emit Result" should be type token (1).
	found := false
	for _, tok := range tokens {
		if tok.tokenType == 1 && tok.length == len("Result") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected type token for 'Result' in emit, got tokens: %+v", tokens)
	}
}

func TestComputeSemanticTokens_EmptySource(t *testing.T) {
	source := []byte(``)
	full, err := arbiter.CompileFull(source)
	if err != nil {
		// Empty source may fail to compile; that's fine.
		return
	}
	tokens := computeSemanticTokens(source, full.Program)
	if len(tokens) != 0 {
		t.Errorf("expected no tokens for empty source, got %+v", tokens)
	}
}
