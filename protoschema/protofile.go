package protoschema

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/linker"
	"google.golang.org/protobuf/reflect/protoreflect"

	"m31labs.dev/arbiter/ir"
)

// FromProtoFile compiles a .proto source file (no protoc toolchain required)
// and synthesizes an Arbiter input schema from the named message, given by its
// fully-qualified name (e.g. "acme.Order"). Imports are resolved relative to
// the file's directory plus the protobuf well-known types.
func FromProtoFile(path, message string) (*ir.InputSchema, error) {
	dir := filepath.Dir(path)
	name := filepath.Base(path)
	c := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: []string{dir},
		}),
	}
	files, err := c.Compile(context.Background(), name)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", path, err)
	}
	md := findMessageInFiles(files, protoreflect.FullName(message))
	if md == nil {
		return nil, fmt.Errorf("message %q not found in %s", message, path)
	}
	return FromMessage(md)
}

// findMessageInFiles searches compiled files for a message by fully-qualified
// name, descending into nested message types.
func findMessageInFiles(files linker.Files, full protoreflect.FullName) protoreflect.MessageDescriptor {
	for _, f := range files {
		if md := findMessageInDescriptors(f.Messages(), full); md != nil {
			return md
		}
	}
	return nil
}

func findMessageInDescriptors(msgs protoreflect.MessageDescriptors, full protoreflect.FullName) protoreflect.MessageDescriptor {
	for i := 0; i < msgs.Len(); i++ {
		m := msgs.Get(i)
		if m.FullName() == full {
			return m
		}
		if nested := findMessageInDescriptors(m.Messages(), full); nested != nil {
			return nested
		}
	}
	return nil
}
