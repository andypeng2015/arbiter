package gostruct

import (
	"testing"

	"m31labs.dev/arbiter/ir"
)

func fieldNames(fs []ir.SchemaField) []string {
	names := make([]string, len(fs))
	for i, f := range fs {
		names[i] = f.Name
	}
	return names
}

func findField(t *testing.T, fs []ir.SchemaField, name string) ir.SchemaField {
	t.Helper()
	for _, f := range fs {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("field %q not present (have %v)", name, fieldNames(fs))
	return ir.SchemaField{}
}

const orderGo = "package acme\n" +
	"import \"time\"\n" +
	"type Order struct {\n" +
	"\tID      string            `arb:\"id\"`\n" +
	"\tTotal   float64           `arb:\"total\"`\n" +
	"\tActive  bool              `arb:\"active\"`\n" +
	"\tCount   int               `arb:\"count\"`\n" +
	"\tTags    []string          `arb:\"tags\"`\n" +
	"\tLabels  map[string]string `arb:\"labels\"`\n" +
	"\tCreated time.Time         `arb:\"created\"`\n" +
	"\tAddr    Address           `arb:\"addr\"`\n" +
	"\tNote    *string           `arb:\"note\"`\n" +
	"\tRenamed string            `arb:\"display_name\"`\n" +
	"\tUntagged int\n" +
	"}\n" +
	"type Address struct {\n" +
	"\tCity string `arb:\"city\"`\n" +
	"}\n"

func TestFromGoSourceMapsFields(t *testing.T) {
	schema, err := FromGoSource([]byte(orderGo), "Order")
	if err != nil {
		t.Fatalf("FromGoSource: %v", err)
	}
	// Untagged field excluded (only arb-tagged fields enter the context).
	if got := len(schema.Fields); got != 10 {
		t.Fatalf("got %d fields %v, want 10", got, fieldNames(schema.Fields))
	}
	check := func(name, base string) {
		if f := findField(t, schema.Fields, name); f.Type.Base != base {
			t.Errorf("%s base = %q, want %q", name, f.Type.Base, base)
		}
	}
	check("id", "string")
	check("total", "number")
	check("active", "boolean")
	check("count", "number")
	check("created", "timestamp")

	if f := findField(t, schema.Fields, "tags"); f.Type.Base != "list" || f.Type.Element == nil || f.Type.Element.Base != "string" {
		t.Errorf("tags = %+v, want list<string>", f.Type)
	}
	if f := findField(t, schema.Fields, "labels"); f.Type.Base != "object" || !f.Type.Open {
		t.Errorf("labels = %+v, want open object", f.Type)
	}
	if f := findField(t, schema.Fields, "addr"); f.Type.Base != "object" || len(f.Children) != 1 || f.Children[0].Name != "city" {
		t.Errorf("addr = %+v children %v, want object{city}", f.Type, fieldNames(f.Children))
	}
	if f := findField(t, schema.Fields, "note"); f.Type.Base != "string" || f.Required {
		t.Errorf("note = %+v required=%v, want optional string", f.Type, f.Required)
	}
	// Renamed via tag.
	findField(t, schema.Fields, "display_name")
}

func TestFromGoSourceUnknownType(t *testing.T) {
	if _, err := FromGoSource([]byte(orderGo), "Nope"); err == nil {
		t.Fatal("want error for type absent from source")
	}
}
