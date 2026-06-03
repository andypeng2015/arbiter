package arbiter

import (
	"strings"
	"testing"

	"m31labs.dev/arbiter/ir"
)

// userInputSchema is the kind of schema protoschema.FromMessage would produce
// for a message User { string email; int32 age; Address address { string city } }.
func userInputSchema() *ir.InputSchema {
	return &ir.InputSchema{Fields: []ir.SchemaField{
		{Name: "email", Type: ir.FieldType{Base: "string"}},
		{Name: "age", Type: ir.FieldType{Base: "number"}},
		{Name: "address", Type: ir.FieldType{Base: "object"}, Children: []ir.SchemaField{
			{Name: "city", Type: ir.FieldType{Base: "string"}},
		}},
	}}
}

func TestWithInputSchemaTypeChecksKnownField(t *testing.T) {
	src := []byte(`rule AdultUS {
    when { age >= 18 and address.city == "NYC" }
    then Allow {}
}`)
	if _, err := Compile(src, WithInputSchema(userInputSchema())); err != nil {
		t.Fatalf("compile with valid field refs against injected schema: %v", err)
	}
}

func TestWithInputSchemaRejectsUnknownField(t *testing.T) {
	src := []byte(`rule R {
    when { unknown_field > 1 }
    then Allow {}
}`)
	_, err := Compile(src, WithInputSchema(userInputSchema()))
	if err == nil {
		t.Fatal("expected compile error referencing a field absent from the injected schema, got nil")
	}
	if !strings.Contains(err.Error(), "unknown_field") {
		t.Fatalf("error should name the offending field, got: %v", err)
	}
}

func TestWithInputSchemaRejectsUnknownNestedField(t *testing.T) {
	src := []byte(`rule R {
    when { address.zipcode == "10001" }
    then Allow {}
}`)
	_, err := Compile(src, WithInputSchema(userInputSchema()))
	if err == nil {
		t.Fatal("expected compile error for unknown nested field address.zipcode, got nil")
	}
}

// An injected schema must coexist with an in-source input{} block, merging
// without conflict (uses the existing checkInputSchemaConflicts path).
func TestWithInputSchemaMergesWithInputBlock(t *testing.T) {
	src := []byte(`input { session: { id: string } }
rule R {
    when { age >= 18 and session.id != "" }
    then Allow {}
}`)
	if _, err := Compile(src, WithInputSchema(userInputSchema())); err != nil {
		t.Fatalf("compile merging injected schema with input{} block: %v", err)
	}
}

// A conflicting type on the same path must be reported.
func TestWithInputSchemaOpenObjectAllowsSubPaths(t *testing.T) {
	// An open object (e.g. a protobuf map) permits any sub-key access even under
	// a closed schema.
	schema := &ir.InputSchema{Fields: []ir.SchemaField{
		{Name: "labels", Type: ir.FieldType{Base: "object", Open: true}},
	}}
	src := []byte(`rule R { when { labels.env == "prod" } then A {} }`)
	if _, err := Compile(src, WithInputSchema(schema)); err != nil {
		t.Fatalf("sub-path access on an open object should be allowed, got: %v", err)
	}
}

func TestWithInputSchemaOpaqueObjectRejectsSubPaths(t *testing.T) {
	// A non-open object with no children is opaque; sub-key access is rejected.
	schema := &ir.InputSchema{Fields: []ir.SchemaField{
		{Name: "addr", Type: ir.FieldType{Base: "object"}},
	}}
	src := []byte(`rule R { when { addr.city == "x" } then A {} }`)
	if _, err := Compile(src, WithInputSchema(schema)); err == nil {
		t.Fatal("sub-path access on an opaque object should be rejected")
	}
}

func TestWithInputSchemaConflictRejected(t *testing.T) {
	src := []byte(`input { age: string }
rule R { when { age >= 18 } then Allow {} }`)
	_, err := Compile(src, WithInputSchema(userInputSchema()))
	if err == nil {
		t.Fatal("expected conflict error: injected age:number vs input{} age:string")
	}
}
