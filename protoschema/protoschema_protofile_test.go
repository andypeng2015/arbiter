package protoschema

import (
	"os"
	"path/filepath"
	"testing"
)

func writeProto(t *testing.T, dir, name, src string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestFromProtoFile(t *testing.T) {
	dir := t.TempDir()
	path := writeProto(t, dir, "order.proto", `syntax = "proto3";
package acme;
message Order {
  string id = 1;
  double total = 2;
  repeated string tags = 3;
}`)

	schema, err := FromProtoFile(path, "acme.Order")
	if err != nil {
		t.Fatalf("FromProtoFile: %v", err)
	}
	if len(schema.Fields) != 3 {
		t.Fatalf("got %d fields %v, want 3", len(schema.Fields), fieldNames(schema.Fields))
	}
	if f := findField(t, schema, "total"); f.Type.Base != "number" {
		t.Errorf("total base = %q, want number", f.Type.Base)
	}
	if f := findField(t, schema, "tags"); f.Type.Base != "list" || f.Type.Element == nil || f.Type.Element.Base != "string" {
		t.Errorf("tags = %+v, want list<string>", f.Type)
	}
	if !schema.Closed {
		// FromProtoFile returns the bare synthesized schema; Closed is set at
		// injection time (WithInputSchema), so we expect it false here.
	}
}

func TestFromProtoFileUnknownMessage(t *testing.T) {
	dir := t.TempDir()
	path := writeProto(t, dir, "order.proto", `syntax = "proto3";
package acme;
message Order { string id = 1; }`)
	if _, err := FromProtoFile(path, "acme.Nope"); err == nil {
		t.Fatal("want error for message absent from .proto, got nil")
	}
}

func TestFromProtoFileBadSyntax(t *testing.T) {
	dir := t.TempDir()
	path := writeProto(t, dir, "bad.proto", `this is not a valid proto file`)
	if _, err := FromProtoFile(path, "acme.Order"); err == nil {
		t.Fatal("want error for invalid .proto, got nil")
	}
}
