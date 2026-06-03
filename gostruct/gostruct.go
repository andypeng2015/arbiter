// Package gostruct synthesizes an Arbiter input schema (ir.InputSchema) from a
// Go struct type, parsed with gotreesitter's embedded Go grammar — no go/types,
// no build context, and no need to import the user's package. Field names come
// from `arb:"..."` struct tags (the same convention the runtime DataFromStruct
// path uses), so the compile-time checker and runtime binding share one source
// of truth. Only arb-tagged fields enter the schema.
package gostruct

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"m31labs.dev/arbiter/ir"
)

// FromStructFile parses a .go source file and synthesizes an input schema from
// the named struct type.
func FromStructFile(path, typeName string) (*ir.InputSchema, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	schema, err := FromGoSource(src, typeName)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return schema, nil
}

// FromGoSource is FromStructFile for an in-memory Go buffer.
func FromGoSource(src []byte, typeName string) (*ir.InputSchema, error) {
	lang := grammars.GoLanguage()
	if lang == nil {
		return nil, fmt.Errorf("go grammar unavailable in this build")
	}
	tree, err := gotreesitter.NewParser(lang).Parse(src)
	if err != nil {
		return nil, fmt.Errorf("parse go: %w", err)
	}
	w := &goWalker{src: src, lang: lang, structs: map[string]*gotreesitter.Node{}}
	w.index(tree.RootNode())

	st := w.structs[typeName]
	if st == nil {
		return nil, fmt.Errorf("struct type %q not found in source", typeName)
	}
	return &ir.InputSchema{Fields: w.fields(st, map[string]bool{typeName: true})}, nil
}

var goScalarBase = map[string]string{
	"string": "string", "bool": "boolean",
	"int": "number", "int8": "number", "int16": "number", "int32": "number", "int64": "number",
	"uint": "number", "uint8": "number", "uint16": "number", "uint32": "number", "uint64": "number", "uintptr": "number",
	"byte": "number", "rune": "number", "float32": "number", "float64": "number",
	"complex64": "number", "complex128": "number",
}

// goWKT maps common qualified Go types to Arbiter base types.
var goWKT = map[string]string{
	"time.Time":     "timestamp",
	"time.Duration": "number",
}

type goWalker struct {
	src     []byte
	lang    *gotreesitter.Language
	structs map[string]*gotreesitter.Node // type name -> struct_type node
}

func (w *goWalker) text(n *gotreesitter.Node) string {
	if n == nil {
		return ""
	}
	return string(w.src[n.StartByte():n.EndByte()])
}

func (w *goWalker) childByType(n *gotreesitter.Node, typ string) *gotreesitter.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c.Type(w.lang) == typ {
			return c
		}
	}
	return nil
}

func lastNamedChild(n *gotreesitter.Node) *gotreesitter.Node {
	if n == nil || n.NamedChildCount() == 0 {
		return nil
	}
	return n.NamedChild(n.NamedChildCount() - 1)
}

// index records every named struct type in the file.
func (w *goWalker) index(n *gotreesitter.Node) {
	if n == nil {
		return
	}
	if n.Type(w.lang) == "type_spec" {
		name := w.text(n.ChildByFieldName("name", w.lang))
		typeNode := n.ChildByFieldName("type", w.lang)
		if name != "" && typeNode != nil && typeNode.Type(w.lang) == "struct_type" {
			w.structs[name] = typeNode
		}
	}
	for i := 0; i < n.NamedChildCount(); i++ {
		w.index(n.NamedChild(i))
	}
}

func (w *goWalker) fields(structType *gotreesitter.Node, seen map[string]bool) []ir.SchemaField {
	list := w.childByType(structType, "field_declaration_list")
	if list == nil {
		return nil
	}
	var out []ir.SchemaField
	for i := 0; i < list.NamedChildCount(); i++ {
		fd := list.NamedChild(i)
		if fd.Type(w.lang) != "field_declaration" {
			continue
		}
		tagNode := fd.ChildByFieldName("tag", w.lang)
		if tagNode == nil {
			continue // untagged fields are not part of the context
		}
		tag := reflect.StructTag(strings.Trim(w.text(tagNode), "`")).Get("arb")
		if tag == "" || tag == "-" {
			continue
		}
		typeNode := fd.ChildByFieldName("type", w.lang)
		if typeNode == nil {
			continue
		}
		ft, children, optional := w.fieldType(typeNode, seen)
		out = insertField(out, strings.Split(tag, "."), ir.SchemaField{
			Type:     ft,
			Children: children,
			Required: !optional,
		})
	}
	return out
}

// insertField places leaf at the dotted path segs, creating/merging intermediate
// object fields so tags like `arb:"user.id"` nest correctly.
func insertField(fields []ir.SchemaField, segs []string, leaf ir.SchemaField) []ir.SchemaField {
	name := segs[0]
	if len(segs) == 1 {
		leaf.Name = name
		return append(fields, leaf)
	}
	for i := range fields {
		if fields[i].Name == name {
			fields[i].Children = insertField(fields[i].Children, segs[1:], leaf)
			return fields
		}
	}
	parent := ir.SchemaField{Name: name, Type: ir.FieldType{Base: "object"}, Required: true}
	parent.Children = insertField(nil, segs[1:], leaf)
	return append(fields, parent)
}

// fieldType maps a Go type node to an Arbiter field type. The third return is
// whether the field is optional (a pointer type).
func (w *goWalker) fieldType(n *gotreesitter.Node, seen map[string]bool) (ir.FieldType, []ir.SchemaField, bool) {
	switch n.Type(w.lang) {
	case "pointer_type":
		ft, children, _ := w.fieldType(lastNamedChild(n), seen)
		return ft, children, true
	case "slice_type", "array_type":
		elemNode := lastNamedChild(n)
		if name := w.text(elemNode); name == "byte" || name == "uint8" {
			return ir.FieldType{Base: "string"}, nil, false // []byte
		}
		ef, _, _ := w.fieldType(elemNode, seen)
		elem := ef
		return ir.FieldType{Base: "list", Element: &elem}, nil, false
	case "map_type":
		return ir.FieldType{Base: "object", Open: true}, nil, false
	case "qualified_type":
		if base, ok := goWKT[strings.TrimSpace(w.text(n))]; ok {
			return ir.FieldType{Base: base}, nil, false
		}
		return ir.FieldType{Base: "object", Open: true}, nil, false
	case "type_identifier":
		name := w.text(n)
		if base, ok := goScalarBase[name]; ok {
			return ir.FieldType{Base: base}, nil, false
		}
		if st, ok := w.structs[name]; ok {
			if seen[name] {
				return ir.FieldType{Base: "object", Open: true}, nil, false // cycle break
			}
			seen[name] = true
			children := w.fields(st, seen)
			delete(seen, name)
			return ir.FieldType{Base: "object"}, children, false
		}
		return ir.FieldType{Base: "object", Open: true}, nil, false // unknown named type
	}
	return ir.FieldType{}, nil, false
}
