package arbiter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/odvcencio/arbiter/ir"
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

// moduleTree holds the resolved import graph for a root program.
type moduleTree struct {
	root     *ir.Program
	rootPath string
	modules  map[string]*ir.Program // namespace → lowered IR
	order    []string               // topological order (imports before importers)
	resolver *moduleResolver
}

// loadModuleTree recursively resolves imports from the root program, producing
// a module tree with all dependencies lowered and ready for merging.
func loadModuleTree(rootProg *ir.Program, rootPath string, resolver *moduleResolver) (*moduleTree, error) {
	tree := &moduleTree{
		root:     rootProg,
		rootPath: rootPath,
		modules:  make(map[string]*ir.Program),
		resolver: resolver,
	}

	// seen tracks resolved file paths that have been fully processed (diamond dedup).
	seen := make(map[string]string) // resolved path → namespace

	// stack tracks file paths currently being processed (cycle detection).
	stack := make(map[string]bool)

	// nsToPath tracks namespace → import path for collision detection.
	nsToPath := make(map[string]string)

	if err := tree.resolve(rootProg.Imports, rootPath, seen, stack, nsToPath); err != nil {
		return nil, err
	}
	return tree, nil
}

// resolve recursively resolves a set of imports.
func (t *moduleTree) resolve(imports []ir.Import, fromPath string, seen map[string]string, stack map[string]bool, nsToPath map[string]string) error {
	for _, imp := range imports {
		namespace := imp.Alias
		if namespace == "" {
			// Default namespace is the last path segment.
			parts := strings.Split(imp.Path, "/")
			namespace = parts[len(parts)-1]
		}

		// Resolve file path.
		_, resolvedPath, err := t.resolver.Resolve(imp.Path, filepath.Dir(fromPath))
		if err != nil {
			return fmt.Errorf("import %q from %s: %w", imp.Path, fromPath, err)
		}

		// Diamond dedup: already resolved via a different import chain.
		if existingNS, ok := seen[resolvedPath]; ok {
			// If the same file is imported under a different namespace, that's
			// fine — we just skip re-processing. The declarations are already
			// in the tree under the first namespace.
			_ = existingNS
			continue
		}

		// Namespace collision detection.
		if prevPath, ok := nsToPath[namespace]; ok {
			return fmt.Errorf("namespace %q conflict: both %q and %q resolve to namespace %q", namespace, prevPath, imp.Path, namespace)
		}

		// Cycle detection.
		if stack[resolvedPath] {
			// Build a readable cycle string.
			return fmt.Errorf("circular import: %s imports %q which is already being processed", fromPath, imp.Path)
		}

		stack[resolvedPath] = true

		// Parse and lower the imported module.
		source, err := os.ReadFile(resolvedPath)
		if err != nil {
			return fmt.Errorf("read import %q: %w", imp.Path, err)
		}
		parsed, err := ParseSource(source)
		if err != nil {
			return fmt.Errorf("parse import %q: %w", imp.Path, err)
		}
		program, err := ir.Lower(parsed.Root, parsed.Source, parsed.Lang)
		if err != nil {
			return fmt.Errorf("lower import %q: %w", imp.Path, err)
		}

		// Recursively resolve imports in this module.
		if err := t.resolve(program.Imports, resolvedPath, seen, stack, nsToPath); err != nil {
			return err
		}

		// Prefix all declarations with the namespace.
		prefixDeclarations(program, namespace)

		// Record in tree.
		seen[resolvedPath] = namespace
		nsToPath[namespace] = imp.Path
		t.modules[namespace] = program
		t.order = append(t.order, namespace)

		delete(stack, resolvedPath)
	}
	return nil
}

// prefixDeclarations prefixes all declaration names in a program with the
// given namespace. Internal references (prereqs, excludes, requires, segments,
// action names) that are unqualified are also prefixed.
func prefixDeclarations(prog *ir.Program, namespace string) {
	prefix := namespace + "."

	for i := range prog.Consts {
		prog.Consts[i].Name = prefix + prog.Consts[i].Name
	}
	for i := range prog.Rules {
		prog.Rules[i].Name = prefix + prog.Rules[i].Name
		// Prefix unqualified prereqs.
		for j := range prog.Rules[i].Prereqs {
			if !strings.Contains(prog.Rules[i].Prereqs[j], ".") {
				prog.Rules[i].Prereqs[j] = prefix + prog.Rules[i].Prereqs[j]
			}
		}
		// Prefix unqualified excludes.
		for j := range prog.Rules[i].Excludes {
			if !strings.Contains(prog.Rules[i].Excludes[j], ".") {
				prog.Rules[i].Excludes[j] = prefix + prog.Rules[i].Excludes[j]
			}
		}
		// Prefix unqualified segment references.
		if prog.Rules[i].Segment != "" && !strings.Contains(prog.Rules[i].Segment, ".") {
			prog.Rules[i].Segment = prefix + prog.Rules[i].Segment
		}
	}
	for i := range prog.Segments {
		prog.Segments[i].Name = prefix + prog.Segments[i].Name
	}
	for i := range prog.Expert {
		prog.Expert[i].Name = prefix + prog.Expert[i].Name
		for j := range prog.Expert[i].Prereqs {
			if !strings.Contains(prog.Expert[i].Prereqs[j], ".") {
				prog.Expert[i].Prereqs[j] = prefix + prog.Expert[i].Prereqs[j]
			}
		}
		for j := range prog.Expert[i].Excludes {
			if !strings.Contains(prog.Expert[i].Excludes[j], ".") {
				prog.Expert[i].Excludes[j] = prefix + prog.Expert[i].Excludes[j]
			}
		}
		if prog.Expert[i].Segment != "" && !strings.Contains(prog.Expert[i].Segment, ".") {
			prog.Expert[i].Segment = prefix + prog.Expert[i].Segment
		}
	}
	for i := range prog.Flags {
		prog.Flags[i].Name = prefix + prog.Flags[i].Name
		for j := range prog.Flags[i].Requires {
			if !strings.Contains(prog.Flags[i].Requires[j], ".") {
				prog.Flags[i].Requires[j] = prefix + prog.Flags[i].Requires[j]
			}
		}
	}
	for i := range prog.Features {
		prog.Features[i].Name = prefix + prog.Features[i].Name
	}
	for i := range prog.FactSchemas {
		prog.FactSchemas[i].Name = prefix + prog.FactSchemas[i].Name
	}
	for i := range prog.OutcomeSchemas {
		prog.OutcomeSchemas[i].Name = prefix + prog.OutcomeSchemas[i].Name
	}
	for i := range prog.Strategies {
		prog.Strategies[i].Name = prefix + prog.Strategies[i].Name
	}
	for i := range prog.Workers {
		prog.Workers[i].Name = prefix + prog.Workers[i].Name
	}
	for i := range prog.Arbiters {
		prog.Arbiters[i].Name = prefix + prog.Arbiters[i].Name
	}
}

// offsetExprIDs shifts all ExprID references in a program by the given offset.
// This is required when merging expression arrays from multiple modules.
func offsetExprIDs(prog *ir.Program, offset ir.ExprID) {
	if offset == 0 {
		return
	}

	// Helper to offset a single ExprID.
	off := func(id *ir.ExprID) {
		*id += offset
	}

	// Offset expression-internal references.
	for i := range prog.Exprs {
		e := &prog.Exprs[i]
		for j := range e.Elems {
			off(&e.Elems[j])
		}
		off(&e.Left)
		off(&e.Right)
		off(&e.Operand)
		off(&e.Value)
		off(&e.Low)
		off(&e.High)
		off(&e.Collection)
		off(&e.Body)
		off(&e.ValueExpr)
		for j := range e.Args {
			off(&e.Args[j])
		}
	}

	// Offset declaration-level ExprID fields.
	for i := range prog.Consts {
		off(&prog.Consts[i].Value)
	}
	for i := range prog.Segments {
		off(&prog.Segments[i].Condition)
	}
	for i := range prog.Rules {
		r := &prog.Rules[i]
		off(&r.Condition)
		for j := range r.Lets {
			off(&r.Lets[j].Value)
		}
		for j := range r.Action.Params {
			off(&r.Action.Params[j].Value)
		}
		if r.Fallback != nil {
			for j := range r.Fallback.Params {
				off(&r.Fallback.Params[j].Value)
			}
		}
	}
	for i := range prog.Strategies {
		for j := range prog.Strategies[i].Candidates {
			c := &prog.Strategies[i].Candidates[j]
			off(&c.Condition)
			for k := range c.Lets {
				off(&c.Lets[k].Value)
			}
			for k := range c.Params {
				off(&c.Params[k].Value)
			}
		}
	}
	for i := range prog.Flags {
		f := &prog.Flags[i]
		for j := range f.Rules {
			off(&f.Rules[j].Condition)
		}
		for j := range f.Defaults {
			off(&f.Defaults[j].Value)
		}
		for j := range f.Variants {
			for k := range f.Variants[j].Params {
				off(&f.Variants[j].Params[k].Value)
			}
		}
	}
	for i := range prog.Expert {
		e := &prog.Expert[i]
		off(&e.Condition)
		for j := range e.Lets {
			off(&e.Lets[j].Value)
		}
		for j := range e.Params {
			off(&e.Params[j].Value)
		}
		for j := range e.SetParams {
			off(&e.SetParams[j].Value)
		}
	}
	for i := range prog.Arbiters {
		for j := range prog.Arbiters[i].Clauses {
			off(&prog.Arbiters[i].Clauses[j].Filter)
		}
	}
}

// mergeModules merges all module IRs into a single program, with imported
// modules in dependency order followed by the root program's declarations.
// Returns an error if cross-module input schema type conflicts are detected.
func mergeModules(tree *moduleTree) (*ir.Program, error) {
	merged := &ir.Program{}
	exprOffset := ir.ExprID(0)

	// Check for input schema type conflicts between the root module and each
	// imported module that also declares an input block.
	for _, ns := range tree.order {
		mod := tree.modules[ns]
		if err := checkInputSchemaConflicts(tree.root.Input, mod.Input); err != nil {
			return nil, fmt.Errorf("import %q: %w", ns, err)
		}
	}

	// Add modules in dependency order (imports first).
	for _, ns := range tree.order {
		mod := tree.modules[ns]
		offsetExprIDs(mod, exprOffset)
		exprOffset += ir.ExprID(len(mod.Exprs))

		merged.Consts = append(merged.Consts, mod.Consts...)
		merged.Features = append(merged.Features, mod.Features...)
		merged.FactSchemas = append(merged.FactSchemas, mod.FactSchemas...)
		merged.OutcomeSchemas = append(merged.OutcomeSchemas, mod.OutcomeSchemas...)
		merged.Strategies = append(merged.Strategies, mod.Strategies...)
		merged.Workers = append(merged.Workers, mod.Workers...)
		merged.Segments = append(merged.Segments, mod.Segments...)
		merged.Rules = append(merged.Rules, mod.Rules...)
		merged.Flags = append(merged.Flags, mod.Flags...)
		merged.Expert = append(merged.Expert, mod.Expert...)
		merged.Arbiters = append(merged.Arbiters, mod.Arbiters...)
		merged.Exprs = append(merged.Exprs, mod.Exprs...)
	}

	// Add root declarations last (with expr offset).
	offsetExprIDs(tree.root, exprOffset)
	merged.Consts = append(merged.Consts, tree.root.Consts...)
	merged.Features = append(merged.Features, tree.root.Features...)
	merged.FactSchemas = append(merged.FactSchemas, tree.root.FactSchemas...)
	merged.OutcomeSchemas = append(merged.OutcomeSchemas, tree.root.OutcomeSchemas...)
	merged.Strategies = append(merged.Strategies, tree.root.Strategies...)
	merged.Workers = append(merged.Workers, tree.root.Workers...)
	merged.Segments = append(merged.Segments, tree.root.Segments...)
	merged.Rules = append(merged.Rules, tree.root.Rules...)
	merged.Flags = append(merged.Flags, tree.root.Flags...)
	merged.Expert = append(merged.Expert, tree.root.Expert...)
	merged.Arbiters = append(merged.Arbiters, tree.root.Arbiters...)
	merged.Exprs = append(merged.Exprs, tree.root.Exprs...)

	// Carry over root's imports and input (informational).
	merged.Imports = tree.root.Imports
	merged.Input = tree.root.Input

	merged.RebuildIndexes()
	return merged, nil
}
