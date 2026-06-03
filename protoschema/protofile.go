package protoschema

import (
	"fmt"
	"os"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"m31labs.dev/arbiter/ir"
)

// FromProtoFile parses a .proto source file with gotreesitter's embedded
// protobuf grammar — no protoc toolchain and no external parser dependency —
// and synthesizes an Arbiter input schema from the named message. The message
// may be fully-qualified ("acme.Order") or bare ("Order").
//
// Imports are not linked: a field whose type is defined in another .proto file
// resolves to an opaque object. For fully-linked schemas spanning imports,
// compile with protoc/buf and use FromFileDescriptorSet instead.
func FromProtoFile(path, message string) (*ir.InputSchema, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	schema, err := FromProtoSource(src, message)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return schema, nil
}

// FromProtoSource is FromProtoFile for an in-memory .proto buffer.
func FromProtoSource(src []byte, message string) (*ir.InputSchema, error) {
	lang := grammars.ProtoLanguage()
	if lang == nil {
		return nil, fmt.Errorf("protobuf grammar unavailable in this build")
	}
	tree, err := gotreesitter.NewParser(lang).Parse(src)
	if err != nil {
		return nil, fmt.Errorf("parse proto: %w", err)
	}
	w := &protoWalker{
		src:      src,
		lang:     lang,
		messages: map[string]*gotreesitter.Node{},
		enums:    map[string]bool{},
	}
	w.index(tree.RootNode())

	body := w.messages[simpleName(message)]
	if body == nil {
		return nil, fmt.Errorf("message %q not found in proto source", message)
	}
	fields, err := w.fields(body, map[string]bool{simpleName(message): true})
	if err != nil {
		return nil, err
	}
	return &ir.InputSchema{Fields: fields}, nil
}

// wktBase maps protobuf well-known message types to Arbiter base types,
// short-circuiting recursion into their internal structure.
var wktBase = map[string]string{
	"google.protobuf.Timestamp":   "timestamp",
	"google.protobuf.Duration":    "number",
	"google.protobuf.StringValue": "string",
	"google.protobuf.BytesValue":  "string",
	"google.protobuf.BoolValue":   "boolean",
	"google.protobuf.Int32Value":  "number",
	"google.protobuf.Int64Value":  "number",
	"google.protobuf.UInt32Value": "number",
	"google.protobuf.UInt64Value": "number",
	"google.protobuf.FloatValue":  "number",
	"google.protobuf.DoubleValue": "number",
}

// protoScalarBase maps protobuf scalar type keywords to Arbiter base types.
var protoScalarBase = map[string]string{
	"double": "number", "float": "number",
	"int32": "number", "int64": "number", "uint32": "number", "uint64": "number",
	"sint32": "number", "sint64": "number", "fixed32": "number", "fixed64": "number",
	"sfixed32": "number", "sfixed64": "number",
	"bool": "boolean", "string": "string", "bytes": "string",
}

type protoWalker struct {
	src      []byte
	lang     *gotreesitter.Language
	messages map[string]*gotreesitter.Node // simple name -> message_body node
	enums    map[string]bool               // simple name -> true
}

func simpleName(qualified string) string {
	if i := strings.LastIndexByte(qualified, '.'); i >= 0 {
		return qualified[i+1:]
	}
	return qualified
}

func (w *protoWalker) text(n *gotreesitter.Node) string {
	if n == nil {
		return ""
	}
	return string(w.src[n.StartByte():n.EndByte()])
}

func (w *protoWalker) childByType(n *gotreesitter.Node, typ string) *gotreesitter.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if c.Type(w.lang) == typ {
			return c
		}
	}
	return nil
}

// index collects every message body and enum name in the tree, including types
// nested inside other messages, so field type references can be resolved.
func (w *protoWalker) index(n *gotreesitter.Node) {
	if n == nil {
		return
	}
	switch n.Type(w.lang) {
	case "message":
		name := strings.TrimSpace(w.text(w.childByType(n, "message_name")))
		body := w.childByType(n, "message_body")
		if name != "" && body != nil {
			w.messages[name] = body
			w.index(body) // register nested messages/enums
		}
		return
	case "enum":
		if name := strings.TrimSpace(w.text(w.childByType(n, "enum_name"))); name != "" {
			w.enums[name] = true
		}
		return
	}
	for i := 0; i < n.NamedChildCount(); i++ {
		w.index(n.NamedChild(i))
	}
}

// fields maps a message_body's fields to schema fields. seen holds message
// names on the current resolution path so recursive types terminate.
func (w *protoWalker) fields(body *gotreesitter.Node, seen map[string]bool) ([]ir.SchemaField, error) {
	var out []ir.SchemaField
	for i := 0; i < body.NamedChildCount(); i++ {
		c := body.NamedChild(i)
		switch c.Type(w.lang) {
		case "field":
			f, ok := w.field(c, seen)
			if ok {
				out = append(out, f)
			}
		case "map_field":
			if name := strings.TrimSpace(w.text(w.childByType(c, "identifier"))); name != "" {
				// A map has dynamic keys — an open object (any sub-key allowed).
				out = append(out, ir.SchemaField{Name: name, Type: ir.FieldType{Base: "object", Open: true}})
			}
		}
	}
	return out, nil
}

func (w *protoWalker) field(field *gotreesitter.Node, seen map[string]bool) (ir.SchemaField, bool) {
	name := strings.TrimSpace(w.text(w.childByType(field, "identifier")))
	typeNode := w.childByType(field, "type")
	if name == "" || typeNode == nil {
		return ir.SchemaField{}, false
	}
	repeated := false
	if toks := strings.Fields(w.text(field)); len(toks) > 0 && toks[0] == "repeated" {
		repeated = true
	}

	ft, children := w.fieldType(typeNode, seen)
	if repeated {
		elem := ft // copy
		return ir.SchemaField{Name: name, Type: ir.FieldType{Base: "list", Element: &elem}}, true
	}
	return ir.SchemaField{Name: name, Type: ft, Children: children, Required: false}, true
}

// fieldType resolves a `type` node to an Arbiter field type, recursing into
// message types (with cycle protection) and treating enums as strings.
func (w *protoWalker) fieldType(typeNode *gotreesitter.Node, seen map[string]bool) (ir.FieldType, []ir.SchemaField) {
	if ref := w.childByType(typeNode, "message_or_enum_type"); ref != nil {
		full := strings.TrimSpace(w.text(ref))
		if base, ok := wktBase[full]; ok {
			return ir.FieldType{Base: base}, nil
		}
		refName := simpleName(full)
		if w.enums[refName] {
			return ir.FieldType{Base: "string"}, nil
		}
		if body, ok := w.messages[refName]; ok {
			if seen[refName] {
				return ir.FieldType{Base: "object", Open: true}, nil // cycle break
			}
			seen[refName] = true
			children, _ := w.fields(body, seen)
			delete(seen, refName)
			return ir.FieldType{Base: "object"}, children
		}
		// Imported / unknown type — open object (its fields aren't visible here).
		return ir.FieldType{Base: "object", Open: true}, nil
	}
	// Scalar.
	if base, ok := protoScalarBase[strings.TrimSpace(w.text(typeNode))]; ok {
		return ir.FieldType{Base: base}, nil
	}
	return ir.FieldType{}, nil
}
