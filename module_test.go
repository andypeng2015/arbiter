package arbiter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
