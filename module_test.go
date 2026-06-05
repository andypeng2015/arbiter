package arbiter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/arbiter/vm"
)

func TestFindManifest(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "fraud", "scoring")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifestPath := filepath.Join(dir, "arbiter.toml")
	if err := os.WriteFile(manifestPath, []byte(`[project]
name = "myproject"
version = "1.0.0"
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Call findManifest from a file inside the subdirectory.
	filePath := filepath.Join(subDir, "scoring.arb")
	manifest, err := findManifest(filePath)
	if err != nil {
		t.Fatalf("findManifest: %v", err)
	}
	if manifest == nil {
		t.Fatal("findManifest = nil, want manifest")
	}
	if manifest.Project.Name != "myproject" {
		t.Fatalf("manifest.Project.Name = %q, want %q", manifest.Project.Name, "myproject")
	}
	if manifest.Project.Version != "1.0.0" {
		t.Fatalf("manifest.Project.Version = %q, want %q", manifest.Project.Version, "1.0.0")
	}
	if manifest.dir != dir {
		t.Fatalf("manifest.dir = %q, want %q", manifest.dir, dir)
	}
}

func TestFindManifestNotFound(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "main.arb")

	manifest, err := findManifest(filePath)
	if err != nil {
		t.Fatalf("findManifest (no manifest) error = %v, want nil", err)
	}
	if manifest != nil {
		t.Fatalf("findManifest (no manifest) = %+v, want nil", manifest)
	}
}

func TestModuleResolver(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "arbiter.toml"), []byte(`[project]
name = "testproj"
version = "0.1.0"
`), 0o644); err != nil {
		t.Fatalf("WriteFile arbiter.toml: %v", err)
	}

	fraudDir := filepath.Join(dir, "fraud")
	if err := os.MkdirAll(fraudDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	scoringContent := []byte(`rule FraudCheck { when { true } then Approved {} }`)
	if err := os.WriteFile(filepath.Join(fraudDir, "scoring.arb"), scoringContent, 0o644); err != nil {
		t.Fatalf("WriteFile scoring.arb: %v", err)
	}

	resolver := newModuleResolver(dir)
	source, resolvedPath, err := resolver.Resolve("fraud/scoring", dir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(source) != string(scoringContent) {
		t.Fatalf("source = %q, want %q", source, scoringContent)
	}
	wantPath := filepath.Join(dir, "fraud", "scoring.arb")
	if resolvedPath != wantPath {
		t.Fatalf("resolvedPath = %q, want %q", resolvedPath, wantPath)
	}
}

func TestModuleResolverNotFound(t *testing.T) {
	dir := t.TempDir()
	resolver := newModuleResolver(dir)

	_, _, err := resolver.Resolve("nonexistent/module", dir)
	if err == nil {
		t.Fatal("expected error resolving non-existent import, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent/module") {
		t.Fatalf("error %q should contain import path %q", err.Error(), "nonexistent/module")
	}
}

// writeModuleFile writes a file inside a temp project directory, creating
// intermediate directories as needed.
func writeModuleFile(t *testing.T, root, relPath, content string) string {
	t.Helper()
	full := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", full, err)
	}
	return full
}

// setupModuleProject creates a temp directory with arbiter.toml.
func setupModuleProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeModuleFile(t, dir, "arbiter.toml", `[project]
name = "testproj"
version = "0.1.0"
`)
	return dir
}

func TestModuleRejectsCircularImport(t *testing.T) {
	dir := setupModuleProject(t)
	writeModuleFile(t, dir, "a.arb", `import "b"
rule ARule { when { true } then A {} }
`)
	writeModuleFile(t, dir, "b.arb", `import "a"
rule BRule { when { true } then B {} }
`)
	main := writeModuleFile(t, dir, "main.arb", `import "a"
rule Main { when { true } then M {} }
`)

	_, err := CompileFullFile(main)
	if err == nil {
		t.Fatal("expected circular import error, got nil")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Fatalf("expected error containing 'circular', got: %v", err)
	}
}

func TestModuleRejectsDuplicateNamespace(t *testing.T) {
	dir := setupModuleProject(t)
	writeModuleFile(t, dir, "a/shared.arb", `rule AShared { when { true } then A {} }`)
	writeModuleFile(t, dir, "b/shared.arb", `rule BShared { when { true } then B {} }`)
	main := writeModuleFile(t, dir, "main.arb", `import "a/shared"
import "b/shared"
rule Main { when { true } then M {} }
`)

	_, err := CompileFullFile(main)
	if err == nil {
		t.Fatal("expected namespace collision error, got nil")
	}
	if !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("expected error containing 'namespace', got: %v", err)
	}
	if !strings.Contains(err.Error(), "shared") {
		t.Fatalf("expected error containing 'shared', got: %v", err)
	}
}

func TestModuleRejectsImportAndIncludeTogether(t *testing.T) {
	dir := setupModuleProject(t)
	writeModuleFile(t, dir, "helper.arb", `rule Helper { when { true } then H {} }`)
	main := writeModuleFile(t, dir, "main.arb", `include "helper.arb"
import "helper"
rule Main { when { true } then M {} }
`)

	_, err := CompileFullFile(main)
	if err == nil {
		t.Fatal("expected error for mixing import and include, got nil")
	}
	if !strings.Contains(err.Error(), "import") && !strings.Contains(err.Error(), "include") {
		t.Fatalf("expected error mentioning import/include conflict, got: %v", err)
	}
}

func TestModuleDiamondDedup(t *testing.T) {
	dir := setupModuleProject(t)
	writeModuleFile(t, dir, "d.arb", `rule DRule { when { true } then D {} }`)
	writeModuleFile(t, dir, "b.arb", `import "d"
rule BRule { when { true } then B {} }
`)
	writeModuleFile(t, dir, "c.arb", `import "d"
rule CRule { when { true } then C {} }
`)
	main := writeModuleFile(t, dir, "main.arb", `import "b"
import "c"
rule Main { when { true } then M {} }
`)

	result, err := CompileFullFile(main)
	if err != nil {
		t.Fatalf("CompileFullFile: %v", err)
	}

	// d's DRule should appear exactly once (namespaced as d.DRule).
	dRuleCount := 0
	for _, rule := range result.Ruleset.Rules {
		ev := vm.NewEvaluator(result.Ruleset, vm.NewStringPool(result.Ruleset.Constants.Strings()))
		name := ev.String(rule.NameIdx)
		if name == "d.DRule" {
			dRuleCount++
		}
	}
	if dRuleCount != 1 {
		t.Fatalf("expected d.DRule exactly once, found %d times", dRuleCount)
	}
}

func TestModuleQualifiedRequires(t *testing.T) {
	dir := setupModuleProject(t)
	writeModuleFile(t, dir, "base.arb", `rule BaseCheck {
	when { user.score >= 700 }
	then Approved { level: "base" }
}
`)
	main := writeModuleFile(t, dir, "main.arb", `import "base"
rule Detail {
	requires base.BaseCheck
	when { user.tier == "gold" }
	then Detailed { level: "detail" }
}
`)

	result, err := CompileFullFile(main)
	if err != nil {
		t.Fatalf("CompileFullFile: %v", err)
	}

	ctx := map[string]any{
		"user": map[string]any{
			"score": 800.0,
			"tier":  "gold",
		},
	}
	dc := DataFromMap(ctx, &Program{Ruleset: result.Ruleset, Segments: result.Segments})
	matched, _, err := EvalGoverned(&Program{Ruleset: result.Ruleset, Segments: result.Segments}, dc, result.Segments, ctx)
	if err != nil {
		t.Fatalf("EvalGoverned: %v", err)
	}
	if len(matched) != 2 {
		names := make([]string, len(matched))
		for i, m := range matched {
			names[i] = m.Name
		}
		t.Fatalf("expected 2 matches, got %d: %v", len(matched), names)
	}
	if matched[0].Name != "base.BaseCheck" {
		t.Fatalf("expected first match = base.BaseCheck, got %s", matched[0].Name)
	}
	if matched[1].Name != "Detail" {
		t.Fatalf("expected second match = Detail, got %s", matched[1].Name)
	}
}

func TestModuleSimpleImport(t *testing.T) {
	dir := setupModuleProject(t)
	writeModuleFile(t, dir, "shared.arb", `const LIMIT = 500
rule SharedRule {
	when { user.score >= LIMIT }
	then Shared { source: "module" }
}
`)
	main := writeModuleFile(t, dir, "main.arb", `import "shared"
rule MainRule {
	when { user.active == true }
	then Main { source: "root" }
}
`)

	result, err := CompileFullFile(main)
	if err != nil {
		t.Fatalf("CompileFullFile: %v", err)
	}
	if len(result.Ruleset.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(result.Ruleset.Rules))
	}

	ctx := map[string]any{
		"user": map[string]any{
			"score":  600.0,
			"active": true,
		},
	}
	prog := &Program{Ruleset: result.Ruleset, Segments: result.Segments}
	dc := DataFromMap(ctx, prog)
	matched, err := Eval(prog, dc)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(matched) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matched))
	}
}

// TestModuleIntraModuleConstRefResolves guards against const references inside
// an imported module silently resolving to null after the module's const
// declarations are namespace-prefixed during merge.
func TestModuleIntraModuleConstRefResolves(t *testing.T) {
	dir := setupModuleProject(t)
	writeModuleFile(t, dir, "shared.arb", `const LIMIT = 500
rule SharedRule {
	when { user.score >= LIMIT }
	then Shared { source: "module" }
}
`)
	main := writeModuleFile(t, dir, "main.arb", `import "shared"
rule MainRule { when { user.score >= 100000 } then Main { source: "root" } }
`)

	result, err := CompileFullFile(main)
	if err != nil {
		t.Fatalf("CompileFullFile: %v", err)
	}
	prog := &Program{Ruleset: result.Ruleset, Segments: result.Segments}

	// score 400 < LIMIT 500 → SharedRule must NOT match. If the const ref
	// dangles to null (the bug), 400 >= null evaluates true and it matches.
	ctx := map[string]any{"user": map[string]any{"score": 400.0}}
	matched, err := Eval(prog, DataFromMap(ctx, prog))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(matched) != 0 {
		t.Fatalf("score 400 < LIMIT 500 must not match; got %d (intra-module const resolved to null?)", len(matched))
	}
}

// TestModuleCrossModuleConstRefResolves guards against a qualified const
// reference (namespace.NAME) into an imported module resolving to null instead
// of the imported constant's value.
func TestModuleCrossModuleConstRefResolves(t *testing.T) {
	dir := setupModuleProject(t)
	writeModuleFile(t, dir, "shared.arb", `const LIMIT = 500
`)
	main := writeModuleFile(t, dir, "main.arb", `import "shared"
rule MainRule {
	when { user.score >= shared.LIMIT }
	then Main { source: "root" }
}
`)

	result, err := CompileFullFile(main)
	if err != nil {
		t.Fatalf("CompileFullFile: %v", err)
	}
	prog := &Program{Ruleset: result.Ruleset, Segments: result.Segments}

	// Below the imported limit → must not match.
	low, err := Eval(prog, DataFromMap(map[string]any{"user": map[string]any{"score": 400.0}}, prog))
	if err != nil {
		t.Fatalf("Eval low: %v", err)
	}
	if len(low) != 0 {
		t.Fatalf("score 400 < shared.LIMIT 500 must not match; got %d (qualified const resolved to null?)", len(low))
	}

	// At/above the imported limit → must match.
	high, err := Eval(prog, DataFromMap(map[string]any{"user": map[string]any{"score": 600.0}}, prog))
	if err != nil {
		t.Fatalf("Eval high: %v", err)
	}
	if len(high) != 1 {
		t.Fatalf("score 600 >= shared.LIMIT 500 must match exactly once; got %d", len(high))
	}
}

// TestModuleImportedOutcomeSchemaValidates guards that outcome (and fact)
// schemas live in a single global namespace across modules, so an imported
// schema still validates the rule actions that reference it by bare name. If
// the schema name were namespace-prefixed while the action name was not, the
// lookup would miss and validation would silently skip.
func TestModuleImportedOutcomeSchemaValidates(t *testing.T) {
	dir := setupModuleProject(t)
	writeModuleFile(t, dir, "shared.arb", `outcome Access { tier: string }
`)
	main := writeModuleFile(t, dir, "main.arb", `import "shared"
rule R { when { true } then Access { tier: "ok", bogus: 1 } }
`)

	_, err := CompileFullFile(main)
	if err == nil {
		t.Fatal("expected validation error for unknown field \"bogus\" against imported outcome schema; got nil (schema namespaced away?)")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("expected error to mention unknown field \"bogus\", got: %v", err)
	}
}

func TestModuleImportWithAlias(t *testing.T) {
	dir := setupModuleProject(t)
	writeModuleFile(t, dir, "fraud/scoring.arb", `rule FraudCheck {
	when { tx.amount > 1000 }
	then Flagged {}
}
`)
	main := writeModuleFile(t, dir, "main.arb", `import "fraud/scoring" as fs
rule Main { when { true } then OK {} }
`)

	result, err := CompileFullFile(main)
	if err != nil {
		t.Fatalf("CompileFullFile: %v", err)
	}

	// The imported rule should be namespaced as "fs.FraudCheck".
	ev := vm.NewEvaluator(result.Ruleset, vm.NewStringPool(result.Ruleset.Constants.Strings()))
	found := false
	for _, rule := range result.Ruleset.Rules {
		name := ev.String(rule.NameIdx)
		if name == "fs.FraudCheck" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected rule named fs.FraudCheck from aliased import")
	}
}
