package arbiter

import (
	"fmt"
	"strings"

	"m31labs.dev/arbiter/ir"
)

// resolvedField is the result of resolving a dotted input path.
type resolvedField struct {
	typ      ir.FieldType
	optional bool // true if any field along the path was optional (Required=false)
}

// resolveInputPath checks whether a dotted path (like "user.age") matches the
// declared input schema. Returns the resolved field if found, nil if the path
// doesn't start with a known input root field, or an error if the path starts
// with an input field but is invalid (unknown sub-field or leaf dereference).
func resolveInputPath(input *ir.InputSchema, path string) (*resolvedField, error) {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, nil
	}

	// Find the top-level field matching parts[0].
	var field *ir.SchemaField
	for i := range input.Fields {
		if input.Fields[i].Name == parts[0] {
			field = &input.Fields[i]
			break
		}
	}
	if field == nil {
		// Top-level field not declared. A closed schema (e.g. bound from a
		// .proto) rejects it; an open in-source input{} block lets existing
		// permissive logic handle it.
		if input.Closed {
			return nil, fmt.Errorf("%q not declared in input schema", path)
		}
		return nil, nil
	}

	optional := !field.Required

	// Walk down the remaining path segments.
	current := field
	for _, part := range parts[1:] {
		if current.Type.Open {
			// Keys of an open object (protobuf map, unresolved message) are not
			// statically known — any sub-path resolves to an unknown type.
			return &resolvedField{typ: ir.FieldType{}, optional: true}, nil
		}
		if len(current.Children) == 0 {
			// Leaf field — cannot dereference further.
			return nil, fmt.Errorf("%q not declared in input schema (field %q is %s, not an object)", path, current.Name, current.Type.Base)
		}
		found := false
		for i := range current.Children {
			if current.Children[i].Name == part {
				current = &current.Children[i]
				if !current.Required {
					optional = true
				}
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%q not declared in input schema", path)
		}
	}

	return &resolvedField{typ: current.Type, optional: optional}, nil
}

// mergeInjectedInputSchema folds an externally-supplied input schema (e.g.
// synthesized from a .proto) into the program's input schema. If the program
// has no input{} block, the injected schema becomes the input schema.
// Otherwise the two are checked for conflicts on overlapping paths and unioned.
func mergeInjectedInputSchema(program *ir.Program, injected *ir.InputSchema) error {
	if injected == nil {
		return nil
	}
	if program.Input == nil {
		// Clone so we don't mutate the caller's schema (e.g. set Closed on it).
		program.Input = &ir.InputSchema{Fields: injected.Fields, Span: injected.Span}
	} else {
		if err := checkInputSchemaConflicts(program.Input, injected); err != nil {
			return err
		}
		program.Input.Fields = mergeSchemaFields(program.Input.Fields, injected.Fields)
	}
	// An explicitly bound schema is authoritative: undeclared top-level fields
	// are now compile errors.
	program.Input.Closed = true
	return nil
}

// mergeSchemaFields unions two field lists by name, recursing into the children
// of same-named object fields. Callers must have verified the lists are
// type-compatible on overlapping paths (see checkInputSchemaConflicts).
func mergeSchemaFields(base, add []ir.SchemaField) []ir.SchemaField {
	out := append([]ir.SchemaField(nil), base...)
	for _, af := range add {
		idx := -1
		for i := range out {
			if out[i].Name == af.Name {
				idx = i
				break
			}
		}
		if idx == -1 {
			out = append(out, af)
			continue
		}
		if len(af.Children) > 0 || len(out[idx].Children) > 0 {
			out[idx].Children = mergeSchemaFields(out[idx].Children, af.Children)
		}
	}
	return out
}

// checkInputSchemaConflicts compares two InputSchema instances for type
// conflicts on overlapping paths. It returns an error on the first conflict
// found. Either schema may be nil (no conflict in that case).
func checkInputSchemaConflicts(a, b *ir.InputSchema) error {
	if a == nil || b == nil {
		return nil
	}
	return checkFieldSliceConflicts(a.Fields, b.Fields, "")
}

func checkFieldSliceConflicts(aFields, bFields []ir.SchemaField, prefix string) error {
	for _, af := range aFields {
		path := af.Name
		if prefix != "" {
			path = prefix + "." + af.Name
		}
		// Find the same name in b.
		var bf *ir.SchemaField
		for i := range bFields {
			if bFields[i].Name == af.Name {
				bf = &bFields[i]
				break
			}
		}
		if bf == nil {
			continue // only in a — no conflict
		}
		// Both have this field. Check type compatibility.
		if af.Type.Base != bf.Type.Base {
			return fmt.Errorf("input schema type conflict on %q: %s vs %s", path, af.Type.Base, bf.Type.Base)
		}
		if af.Type.Dimension != bf.Type.Dimension {
			return fmt.Errorf("input schema type conflict on %q: dimension %s vs %s", path, af.Type.Dimension, bf.Type.Dimension)
		}
		// Recurse into nested objects.
		if len(af.Children) > 0 || len(bf.Children) > 0 {
			if err := checkFieldSliceConflicts(af.Children, bf.Children, path); err != nil {
				return err
			}
		}
	}
	return nil
}
