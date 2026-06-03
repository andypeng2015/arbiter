package ir_test

import (
	"testing"

	arbiter "m31labs.dev/arbiter"
	"m31labs.dev/arbiter/ir"
)

// lowerSourceAllowErrors parses and lowers, returning the error without failing the test.
func lowerSourceAllowErrors(t *testing.T, source string) (*ir.Program, error) {
	t.Helper()
	parsed, err := arbiter.ParseSource([]byte(source))
	if err != nil {
		t.Fatalf("ParseSource: %v", err)
	}
	return ir.Lower(parsed.Root, parsed.Source, parsed.Lang)
}

func TestLowerImport(t *testing.T) {
	program := lowerSource(t, `import "fraud/scoring"`)

	if got := len(program.Imports); got != 1 {
		t.Fatalf("len(Imports) = %d, want 1", got)
	}
	imp := program.Imports[0]
	if imp.Path != "fraud/scoring" {
		t.Fatalf("Import.Path = %q, want %q", imp.Path, "fraud/scoring")
	}
	if imp.Alias != "" {
		t.Fatalf("Import.Alias = %q, want empty", imp.Alias)
	}
}

func TestLowerImportAlias(t *testing.T) {
	program := lowerSource(t, `import "fraud/scoring" as fs`)

	if got := len(program.Imports); got != 1 {
		t.Fatalf("len(Imports) = %d, want 1", got)
	}
	imp := program.Imports[0]
	if imp.Path != "fraud/scoring" {
		t.Fatalf("Import.Path = %q, want %q", imp.Path, "fraud/scoring")
	}
	if imp.Alias != "fs" {
		t.Fatalf("Import.Alias = %q, want %q", imp.Alias, "fs")
	}
}

func TestLowerImportMultiple(t *testing.T) {
	program := lowerSource(t, `
import "fraud/scoring" as fs
import "kyc/verify"
`)

	if got := len(program.Imports); got != 2 {
		t.Fatalf("len(Imports) = %d, want 2", got)
	}
	if program.Imports[0].Alias != "fs" {
		t.Fatalf("Imports[0].Alias = %q, want fs", program.Imports[0].Alias)
	}
	if program.Imports[1].Path != "kyc/verify" {
		t.Fatalf("Imports[1].Path = %q, want kyc/verify", program.Imports[1].Path)
	}
}

func TestLowerInput(t *testing.T) {
	program := lowerSource(t, `input {
    user: {
        id: string
        age: number
        balance: decimal<USD>
    }
    active: boolean
}`)

	if program.Input == nil {
		t.Fatal("program.Input = nil, want InputSchema")
	}
	if got := len(program.Input.Fields); got != 2 {
		t.Fatalf("len(Input.Fields) = %d, want 2", got)
	}

	userField := program.Input.Fields[0]
	if userField.Name != "user" {
		t.Fatalf("Input.Fields[0].Name = %q, want user", userField.Name)
	}
	if userField.Type.Base != "object" {
		t.Fatalf("user field Type.Base = %q, want object", userField.Type.Base)
	}
	if got := len(userField.Children); got != 3 {
		t.Fatalf("len(user.Children) = %d, want 3", got)
	}
	if userField.Children[0].Name != "id" || userField.Children[0].Type.Base != "string" {
		t.Fatalf("user.Children[0] = %+v, want id:string", userField.Children[0])
	}
	if userField.Children[1].Name != "age" || userField.Children[1].Type.Base != "number" {
		t.Fatalf("user.Children[1] = %+v, want age:number", userField.Children[1])
	}
	balanceField := userField.Children[2]
	if balanceField.Name != "balance" || balanceField.Type.Base != "decimal" || balanceField.Type.Dimension != "USD" {
		t.Fatalf("user.Children[2] = %+v, want balance:decimal<USD>", balanceField)
	}

	activeField := program.Input.Fields[1]
	if activeField.Name != "active" {
		t.Fatalf("Input.Fields[1].Name = %q, want active", activeField.Name)
	}
	if activeField.Type.Base != "boolean" {
		t.Fatalf("active field Type.Base = %q, want boolean", activeField.Type.Base)
	}
	if len(activeField.Children) != 0 {
		t.Fatalf("active field should have no children, got %d", len(activeField.Children))
	}
}

func TestLowerInputFromProto(t *testing.T) {
	program := lowerSource(t, `input from proto "user.proto" message "acme.User"`)

	if program.InputRef == nil {
		t.Fatal("program.InputRef = nil, want a proto input ref")
	}
	if program.InputRef.Kind != "proto" {
		t.Fatalf("InputRef.Kind = %q, want proto", program.InputRef.Kind)
	}
	if program.InputRef.Path != "user.proto" {
		t.Fatalf("InputRef.Path = %q, want user.proto", program.InputRef.Path)
	}
	if program.InputRef.Message != "acme.User" {
		t.Fatalf("InputRef.Message = %q, want acme.User", program.InputRef.Message)
	}
	if program.Input != nil {
		t.Fatal("program.Input should be nil when input is declared via a proto ref")
	}
}

func TestLowerInputFromGo(t *testing.T) {
	program := lowerSource(t, `input from go "order.go" type "Order"`)

	if program.InputRef == nil {
		t.Fatal("program.InputRef = nil, want a go input ref")
	}
	if program.InputRef.Kind != "go" {
		t.Fatalf("InputRef.Kind = %q, want go", program.InputRef.Kind)
	}
	if program.InputRef.Path != "order.go" {
		t.Fatalf("InputRef.Path = %q, want order.go", program.InputRef.Path)
	}
	if program.InputRef.Message != "Order" {
		t.Fatalf("InputRef.Message = %q, want Order", program.InputRef.Message)
	}
}

func TestLowerInputOptionalField(t *testing.T) {
	program := lowerSource(t, `input { user: { tier?: string } }`)

	if program.Input == nil {
		t.Fatal("program.Input = nil")
	}
	if len(program.Input.Fields) != 1 {
		t.Fatalf("len(Input.Fields) = %d, want 1", len(program.Input.Fields))
	}
	userField := program.Input.Fields[0]
	if len(userField.Children) != 1 {
		t.Fatalf("len(user.Children) = %d, want 1", len(userField.Children))
	}
	tierField := userField.Children[0]
	if tierField.Name != "tier" {
		t.Fatalf("tier field Name = %q, want tier", tierField.Name)
	}
	if tierField.Required {
		t.Fatal("tier field should be optional (Required=false)")
	}
	if tierField.Type.Base != "string" {
		t.Fatalf("tier field Type.Base = %q, want string", tierField.Type.Base)
	}
}

func TestLowerInputListType(t *testing.T) {
	program := lowerSource(t, `input { tags: list<string> }`)

	if program.Input == nil {
		t.Fatal("program.Input = nil")
	}
	if len(program.Input.Fields) != 1 {
		t.Fatalf("len(Input.Fields) = %d, want 1", len(program.Input.Fields))
	}
	tagsField := program.Input.Fields[0]
	if tagsField.Type.Base != "list" {
		t.Fatalf("tags field Type.Base = %q, want list", tagsField.Type.Base)
	}
	if tagsField.Type.Element == nil {
		t.Fatal("tags field Type.Element = nil, want *FieldType{Base:string}")
	}
	if tagsField.Type.Element.Base != "string" {
		t.Fatalf("tags field Type.Element.Base = %q, want string", tagsField.Type.Element.Base)
	}
}

func TestLowerInputListOfDecimal(t *testing.T) {
	program := lowerSource(t, `input { amounts: list<decimal<USD>> }`)

	if program.Input == nil {
		t.Fatal("program.Input = nil")
	}
	if len(program.Input.Fields) != 1 {
		t.Fatalf("len(Input.Fields) = %d, want 1", len(program.Input.Fields))
	}
	field := program.Input.Fields[0]
	if field.Type.Base != "list" {
		t.Fatalf("field Type.Base = %q, want list", field.Type.Base)
	}
	elem := field.Type.Element
	if elem == nil || elem.Base != "decimal" || elem.Dimension != "USD" {
		t.Fatalf("field Type.Element = %+v, want decimal<USD>", elem)
	}
}

func TestLowerInputDuplicateRejects(t *testing.T) {
	_, err := lowerSourceAllowErrors(t, `input { x: string }
input { y: number }`)

	if err == nil {
		t.Fatal("expected error for duplicate input declaration, got nil")
	}
}

func TestLowerInputRequiredByDefault(t *testing.T) {
	program := lowerSource(t, `input { name: string }`)

	if program.Input == nil {
		t.Fatal("program.Input = nil")
	}
	if len(program.Input.Fields) != 1 {
		t.Fatalf("len(Input.Fields) = %d, want 1", len(program.Input.Fields))
	}
	if !program.Input.Fields[0].Required {
		t.Fatal("name field should be required by default")
	}
}
