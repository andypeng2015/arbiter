package arbiter

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Manifest holds the parsed contents of an arbiter.toml project manifest.
type Manifest struct {
	Project struct {
		Name    string `toml:"name"`
		Version string `toml:"version"`
	} `toml:"project"`
	dir string // directory containing arbiter.toml (not serialized)
}

// findManifest walks up from the given file path, checking each directory for
// an arbiter.toml manifest. Returns nil with no error if none is found.
// Stops at the filesystem root.
func findManifest(fromPath string) (*Manifest, error) {
	dir := filepath.Dir(fromPath)
	for {
		manifestPath := filepath.Join(dir, "arbiter.toml")
		if _, err := os.Stat(manifestPath); err == nil {
			m, err := parseManifest(manifestPath)
			if err != nil {
				return nil, err
			}
			m.dir = dir
			return m, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root.
			return nil, nil
		}
		dir = parent
	}
}

// parseManifest reads and parses a TOML file into a Manifest.
func parseManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	return &m, nil
}

// moduleResolver resolves import paths relative to a project root directory.
type moduleResolver struct {
	root string // manifest directory (project root)
}

// newModuleResolver creates a resolver anchored at the given project root.
func newModuleResolver(root string) *moduleResolver {
	return &moduleResolver{root: root}
}

// Resolve maps an import path like "fraud/scoring" to its .arb file under
// the project root. Implements the IncludeResolver interface shape.
func (r *moduleResolver) Resolve(importPath, basePath string) ([]byte, string, error) {
	resolved := filepath.Join(r.root, filepath.FromSlash(importPath)+".arb")
	source, err := os.ReadFile(resolved)
	if err != nil {
		return nil, "", fmt.Errorf("cannot resolve import %q: %w", importPath, err)
	}
	return source, resolved, nil
}
