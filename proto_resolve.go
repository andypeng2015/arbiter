package arbiter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/arbiter/ir"
	"m31labs.dev/arbiter/protoschema"
)

// resolveInputRef resolves an `input from proto "<path>" message "<name>"`
// declaration into a closed input schema, merged onto the program. A relative
// path is resolved against baseDir (the .arb file's directory). A ".proto"
// path is parsed in-process via gotreesitter; anything else is treated as a
// compiled FileDescriptorSet.
func resolveInputRef(program *ir.Program, baseDir string) error {
	ref := program.InputRef
	if ref == nil {
		return nil
	}
	if ref.Message == "" {
		return fmt.Errorf("input from %s: a message name is required", ref.Kind)
	}

	path := ref.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}

	var (
		schema *ir.InputSchema
		err    error
	)
	if strings.HasSuffix(ref.Path, ".proto") {
		schema, err = protoschema.FromProtoFile(path, ref.Message)
	} else {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("input from proto: %w", readErr)
		}
		schema, err = protoschema.FromFileDescriptorSet(data, ref.Message)
	}
	if err != nil {
		return fmt.Errorf("input from proto %q: %w", ref.Path, err)
	}
	return mergeInjectedInputSchema(program, schema)
}
