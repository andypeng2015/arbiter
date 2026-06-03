package arbiter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/arbiter/gostruct"
	"m31labs.dev/arbiter/ir"
	"m31labs.dev/arbiter/protoschema"
)

// resolveInputRef resolves an `input from proto|go "<path>" ...` declaration
// into a closed input schema, merged onto the program. A relative path is
// resolved against baseDir (the .arb file's directory). Both foreign-schema
// sources are parsed in-process with gotreesitter — no external toolchain.
func resolveInputRef(program *ir.Program, baseDir string) error {
	ref := program.InputRef
	if ref == nil {
		return nil
	}
	if ref.Message == "" {
		return fmt.Errorf("input from %s: a name is required", ref.Kind)
	}

	path := ref.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}

	var (
		schema *ir.InputSchema
		err    error
	)
	switch ref.Kind {
	case "go":
		schema, err = gostruct.FromStructFile(path, ref.Message)
	case "proto":
		if strings.HasSuffix(ref.Path, ".proto") {
			schema, err = protoschema.FromProtoFile(path, ref.Message)
		} else {
			// Compiled FileDescriptorSet (protoc/buf --descriptor_set_out).
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("input from proto: %w", readErr)
			}
			schema, err = protoschema.FromFileDescriptorSet(data, ref.Message)
		}
	default:
		return fmt.Errorf("input from %s: unsupported source kind", ref.Kind)
	}
	if err != nil {
		return fmt.Errorf("input from %s %q: %w", ref.Kind, ref.Path, err)
	}
	return mergeInjectedInputSchema(program, schema)
}
