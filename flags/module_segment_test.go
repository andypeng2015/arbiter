package flags

import (
	"os"
	"path/filepath"
	"testing"

	"m31labs.dev/arbiter"
)

// TestImportedFlagSegmentResolves guards against a flag rule's segment
// reference dangling after the module merge namespaces the segment declaration.
// The flag and its segment live in an imported module; an entity that belongs
// to the segment must receive the targeted variant rather than falling through
// to the default.
func TestImportedFlagSegmentResolves(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "arbiter.toml"),
		[]byte("[project]\nname = \"t\"\nversion = \"0.1.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFlagTestFile(t, dir, "lib/access.arb", `segment enterprise {
	user.plan == "enterprise"
}
flag checkout_v2 type boolean default false {
	when enterprise then true
}
`)
	main := writeFlagTestFile(t, dir, "main.arb", `import "lib/access"
flag noop type boolean default false {
	when always then false
}
segment always {
	true
}
`)

	_, parsed, err := arbiter.LoadFileParsed(main)
	if err != nil {
		t.Fatalf("LoadFileParsed: %v", err)
	}
	full, err := arbiter.CompileFullFile(main)
	if err != nil {
		t.Fatalf("CompileFullFile: %v", err)
	}
	f, err := LoadParsed(parsed, full)
	if err != nil {
		t.Fatalf("LoadParsed: %v", err)
	}

	// The imported flag is namespaced as access.checkout_v2. An enterprise user
	// must get the targeted variant "true"; a dangling segment ref yields the
	// default "false".
	got := f.VariantName("access.checkout_v2", map[string]any{
		"user": map[string]any{"plan": "enterprise"},
	})
	if got != "true" {
		t.Fatalf("expected imported segment-targeted flag = true, got %q (segment ref dangled?)", got)
	}
}
