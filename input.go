package arbiter

import (
	"fmt"
	"strings"

	"github.com/odvcencio/arbiter/ir"
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
		// Not an input field — let existing logic handle it.
		return nil, nil
	}

	optional := !field.Required

	// Walk down the remaining path segments.
	current := field
	for _, part := range parts[1:] {
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
