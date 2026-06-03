package arbiter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// In-language `input from go` binds .arb type-checking to a sibling Go struct.
func TestCompileFileInputFromGo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "order.go"), []byte("package acme\n\ntype Order struct {\n\tID    string  `arb:\"id\"`\n\tTotal float64 `arb:\"total\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	arbPath := filepath.Join(dir, "rules.arb")

	valid := "input from go \"order.go\" type \"Order\"\nrule BigOrder { when { total >= 100 and id != \"\" } then Flag { why: \"big\" } }"
	if err := os.WriteFile(arbPath, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileFile(arbPath); err != nil {
		t.Fatalf("valid .arb with `input from go` should compile: %v", err)
	}

	typo := "input from go \"order.go\" type \"Order\"\nrule BigOrder { when { totl >= 100 } then Flag { why: \"big\" } }"
	if err := os.WriteFile(arbPath, []byte(typo), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := CompileFile(arbPath)
	if err == nil || !strings.Contains(err.Error(), "totl") {
		t.Fatalf("typo'd field 'totl' should be rejected via the bound Go struct, got: %v", err)
	}
}

// In-language `input from proto` binds .arb type-checking to a sibling .proto,
// resolved relative to the .arb file at compile time.
func TestCompileFileInputFromProto(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "order.proto"), []byte(`syntax = "proto3";
package acme;
message Order { string id = 1; double total = 2; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	arbPath := filepath.Join(dir, "rules.arb")

	valid := `input from proto "order.proto" message "acme.Order"
rule BigOrder { when { total >= 100 and id != "" } then Flag { why: "big" } }`
	if err := os.WriteFile(arbPath, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileFile(arbPath); err != nil {
		t.Fatalf("valid .arb with `input from proto` should compile: %v", err)
	}

	typo := `input from proto "order.proto" message "acme.Order"
rule BigOrder { when { totl >= 100 } then Flag { why: "big" } }`
	if err := os.WriteFile(arbPath, []byte(typo), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := CompileFile(arbPath)
	if err == nil || !strings.Contains(err.Error(), "totl") {
		t.Fatalf("typo'd field 'totl' should be rejected via the bound proto, got: %v", err)
	}
}
