// Package protoschema synthesizes an Arbiter input schema (ir.InputSchema)
// from a protobuf message descriptor, so .arb field references can be
// type-checked at compile time against a .proto the user already owns —
// the same ergonomic cel-go provides via cel.TypeDescs.
//
// It lives in its own package so the core arbiter package stays free of a
// protobuf dependency; callers opt in by passing the synthesized schema to
// arbiter.Compile via the WithInputSchema option.
package protoschema

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"m31labs.dev/arbiter/ir"
)

// FromFileDescriptorSet synthesizes an Arbiter input schema from a serialized
// protobuf FileDescriptorSet — the output of `protoc --descriptor_set_out` or
// `buf build -o set.binpb` — selecting the message by its fully-qualified name
// (e.g. "acme.User"). This is the file-driven path: bind .arb type-checking to
// a compiled .pb without a protoc toolchain at compile time.
func FromFileDescriptorSet(data []byte, message string) (*ir.InputSchema, error) {
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("parse descriptor set: %w", err)
	}
	files, err := protodesc.NewFiles(&set)
	if err != nil {
		return nil, fmt.Errorf("build descriptors: %w", err)
	}
	desc, err := files.FindDescriptorByName(protoreflect.FullName(message))
	if err != nil {
		return nil, fmt.Errorf("message %q not found in descriptor set: %w", message, err)
	}
	md, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		return nil, fmt.Errorf("%q is not a message", message)
	}
	return FromMessage(md)
}

// FromMessage synthesizes an Arbiter input schema from a protobuf message
// descriptor. Every message field becomes a schema field whose type maps onto
// Arbiter's type system.
func FromMessage(md protoreflect.MessageDescriptor) (*ir.InputSchema, error) {
	// seen tracks message types on the current resolution path. The root is not
	// pre-seeded, so a top-level message expands once before any self-reference
	// is broken one level deeper.
	fields, err := messageFields(md, map[protoreflect.FullName]bool{})
	if err != nil {
		return nil, err
	}
	return &ir.InputSchema{Fields: fields}, nil
}

// messageFields maps every field of a message. seen holds the message types on
// the current resolution path so recursive/self-referential messages terminate.
func messageFields(md protoreflect.MessageDescriptor, seen map[protoreflect.FullName]bool) ([]ir.SchemaField, error) {
	fds := md.Fields()
	out := make([]ir.SchemaField, 0, fds.Len())
	for i := 0; i < fds.Len(); i++ {
		fd := fds.Get(i)
		mt, err := fieldType(fd, seen)
		if err != nil {
			return nil, err
		}
		out = append(out, ir.SchemaField{
			Name:     string(fd.Name()),
			Type:     mt.typ,
			Required: false, // proto3 fields are presence-optional; null is allowed
			Children: mt.children,
		})
	}
	return out, nil
}

// mappedType is a resolved field type plus any nested children (for objects).
type mappedType struct {
	typ      ir.FieldType
	children []ir.SchemaField
}

// fieldType maps a single protobuf field to an Arbiter field type.
func fieldType(fd protoreflect.FieldDescriptor, seen map[protoreflect.FullName]bool) (mappedType, error) {
	// Maps are repeated under the hood — check before IsList. A map's keys aren't
	// statically known, so it is an open object.
	if fd.IsMap() {
		return mappedType{typ: ir.FieldType{Base: "object", Open: true}}, nil
	}
	if fd.IsList() {
		elem, err := elementType(fd)
		if err != nil {
			return mappedType{}, err
		}
		return mappedType{typ: ir.FieldType{Base: "list", Element: elem}}, nil
	}
	return singularType(fd, seen)
}

// singularType maps a non-repeated field, recursing into message types.
func singularType(fd protoreflect.FieldDescriptor, seen map[protoreflect.FullName]bool) (mappedType, error) {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		md := fd.Message()
		// Well-known types map to a scalar rather than their internal structure.
		if base, ok := wktBase[string(md.FullName())]; ok {
			return mappedType{typ: ir.FieldType{Base: base}}, nil
		}
		// Cycle break: a message type already on the resolution path is emitted
		// as an open object rather than recursing forever.
		if seen[md.FullName()] {
			return mappedType{typ: ir.FieldType{Base: "object", Open: true}}, nil
		}
		seen[md.FullName()] = true
		children, err := messageFields(md, seen)
		delete(seen, md.FullName())
		if err != nil {
			return mappedType{}, err
		}
		return mappedType{typ: ir.FieldType{Base: "object"}, children: children}, nil
	case protoreflect.EnumKind:
		// Arbiter has no enum type; enum values are referenced by name as strings.
		return mappedType{typ: ir.FieldType{Base: "string"}}, nil
	default:
		return scalarType(fd.Kind())
	}
}

// elementType maps a repeated field's element type into a *ir.FieldType.
// List elements cannot carry nested object structure in the IR, so repeated
// message fields become list<object> with an opaque element.
func elementType(fd protoreflect.FieldDescriptor) (*ir.FieldType, error) {
	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return &ir.FieldType{Base: "object"}, nil
	case protoreflect.EnumKind:
		return &ir.FieldType{Base: "string"}, nil
	default:
		mt, err := scalarType(fd.Kind())
		if err != nil {
			return nil, err
		}
		return &mt.typ, nil
	}
}

// scalarType maps a protobuf scalar kind to an Arbiter base type.
func scalarType(k protoreflect.Kind) (mappedType, error) {
	switch k {
	case protoreflect.BoolKind:
		return mappedType{typ: ir.FieldType{Base: "boolean"}}, nil
	case protoreflect.StringKind, protoreflect.BytesKind:
		return mappedType{typ: ir.FieldType{Base: "string"}}, nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Uint32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Uint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Fixed32Kind,
		protoreflect.Sfixed64Kind, protoreflect.Fixed64Kind,
		protoreflect.FloatKind, protoreflect.DoubleKind:
		return mappedType{typ: ir.FieldType{Base: "number"}}, nil
	}
	// Non-scalar kinds (message, enum, group) are handled by later increments.
	return mappedType{typ: ir.FieldType{}}, nil
}
