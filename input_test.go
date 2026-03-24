package arbiter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInputSchemaRejectsUnknownPath ensures that referencing a field not in the
// input schema produces a compile error.
func TestInputSchemaRejectsUnknownPath(t *testing.T) {
	_, err := CompileFull([]byte(`
input {
    user: {
        id: string
    }
}
rule Check {
    when { user.email == "x@y.com" }
    then OK {}
}
`))
	if err == nil {
		t.Fatal("expected compile error for undeclared input field, got nil")
	}
	if !strings.Contains(err.Error(), "not declared in input schema") {
		t.Fatalf("expected 'not declared in input schema', got: %v", err)
	}
}

// TestInputSchemaAcceptsValidPath ensures that a valid reference compiles fine.
func TestInputSchemaAcceptsValidPath(t *testing.T) {
	_, err := CompileFull([]byte(`
input {
    user: {
        id: string
        age: number
    }
}
rule Check {
    when { user.age > 18 }
    then Adult {}
}
`))
	if err != nil {
		t.Fatalf("expected no error for valid input path, got: %v", err)
	}
}

// TestInputSchemaRejectsTypeMismatch verifies that using a decimal<USD> field
// in a comparison with an incompatible literal produces an error.
func TestInputSchemaRejectsTypeMismatch(t *testing.T) {
	_, err := CompileFull([]byte(`
input {
    user: {
        balance: decimal<USD>
    }
}
rule Check {
    when { user.balance > 28 C }
    then OK {}
}
`))
	if err == nil {
		t.Fatal("expected type mismatch error, got nil")
	}
}

// TestNoInputSchemaAllowsAnyPath verifies that without an input block, any
// dotted path is allowed (v1.0 behavior preserved).
func TestNoInputSchemaAllowsAnyPath(t *testing.T) {
	_, err := CompileFull([]byte(`
rule Check {
    when { anything.goes.here > 5 }
    then OK {}
}
`))
	if err != nil {
		t.Fatalf("expected no error without input schema, got: %v", err)
	}
}

// TestInputSchemaOptionalFieldNullCheck verifies that an optional field can be
// null-checked and then used without compile error.
func TestInputSchemaOptionalFieldNullCheck(t *testing.T) {
	_, err := CompileFull([]byte(`
input {
    user: {
        tier?: string
    }
}
rule Check {
    when { user.tier is not null and user.tier == "premium" }
    then Premium {}
}
`))
	if err != nil {
		t.Fatalf("expected no error for optional field null check, got: %v", err)
	}
}

// TestInputSchemaNestedObject verifies deeply nested valid paths compile fine.
func TestInputSchemaNestedObject(t *testing.T) {
	_, err := CompileFull([]byte(`
input {
    user: {
        address: {
            zip: string
        }
    }
}
rule Check {
    when { user.address.zip == "90210" }
    then OK {}
}
`))
	if err != nil {
		t.Fatalf("expected no error for nested input path, got: %v", err)
	}
}

// TestInputSchemaRejectsNestedUnknown verifies that referencing a non-existent
// nested field is caught at compile time.
func TestInputSchemaRejectsNestedUnknown(t *testing.T) {
	_, err := CompileFull([]byte(`
input {
    user: {
        address: {
            zip: string
        }
    }
}
rule Check {
    when { user.address.city == "LA" }
    then OK {}
}
`))
	if err == nil {
		t.Fatal("expected compile error for undeclared nested input field, got nil")
	}
	if !strings.Contains(err.Error(), "not declared in input schema") {
		t.Fatalf("expected 'not declared in input schema', got: %v", err)
	}
}

// TestInputSchemaCrossModuleIndependent verifies that each module validates
// against its own input schema independently.
func TestInputSchemaCrossModuleIndependent(t *testing.T) {
	dir := setupModuleProject(t)

	writeModuleFile(t, dir, "scoring.arb", `
input {
    tx: {
        amount: number
    }
}
rule FraudCheck {
    when { tx.amount > 1000 }
    then Flagged {}
}
`)
	main := writeModuleFile(t, dir, "main.arb", `
input {
    user: {
        score: number
    }
}
import "scoring"
rule MainRule {
    when { user.score >= 700 }
    then Approved {}
}
`)

	_, err := CompileFullFile(main)
	if err != nil {
		t.Fatalf("expected no error for cross-module independent inputs, got: %v", err)
	}
}

// TestInputSchemaRejectsCrossModuleTypeConflict verifies that when a root
// module and an imported module both declare overlapping input paths with
// incompatible types, compilation fails.
func TestInputSchemaRejectsCrossModuleTypeConflict(t *testing.T) {
	dir := setupModuleProject(t)

	writeModuleFile(t, dir, "lib.arb", `
input {
    user: {
        id: number
    }
}
rule LibRule {
    when { user.id > 0 }
    then LibOK {}
}
`)
	main := writeModuleFile(t, dir, "main.arb", `
input {
    user: {
        id: string
    }
}
import "lib"
rule MainRule {
    when { user.id == "abc" }
    then MainOK {}
}
`)

	_, err := CompileFullFile(main)
	if err == nil {
		t.Fatal("expected type conflict error for cross-module input schemas, got nil")
	}
	if !strings.Contains(err.Error(), "type conflict") && !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict error, got: %v", err)
	}
}

// TestInputSchemaLocalTakesPrecedence verifies that local rule names shadow
// imported namespace paths according to spec section 1.4 resolution order.
func TestInputSchemaLocalTakesPrecedence(t *testing.T) {
	dir := setupModuleProject(t)

	writeModuleFile(t, dir, "user.arb", `
rule UserRule {
    when { true }
    then UserOK {}
}
`)
	// "user" is both an imported namespace and a local input field root.
	// Local rule validation should use the local input schema for "user.score",
	// not try to interpret "user" as the imported namespace.
	main := writeModuleFile(t, dir, "main.arb", `
input {
    user: {
        score: number
    }
}
import "user"
rule MainRule {
    when { user.score >= 700 }
    then Approved {}
}
`)

	_, err := CompileFullFile(main)
	if err != nil {
		t.Fatalf("expected no error when local input field shadows imported namespace, got: %v", err)
	}
}

// TestInputSchemaRejectsLeafDereference verifies that trying to access a field
// on a non-object (leaf) type produces an error.
func TestInputSchemaRejectsLeafDereference(t *testing.T) {
	_, err := CompileFull([]byte(`
input {
    user: {
        age: number
    }
}
rule Check {
    when { user.age.years > 18 }
    then OK {}
}
`))
	if err == nil {
		t.Fatal("expected error for dereferencing a leaf field, got nil")
	}
	if !strings.Contains(err.Error(), "not declared in input schema") {
		t.Fatalf("expected 'not declared in input schema', got: %v", err)
	}
}

// helper — ensure temp module files land in a dir that CompileFullFile can find.
func writeInputTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
	return path
}
