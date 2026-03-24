package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/ir"
)

type importSite struct {
	Path         string
	Alias        string
	Line         int
	Character    int
	ResolvedPath string
	ResolveErr   error
}

type overlayModuleResolver struct {
	root     string
	overlays map[string]string
}

func (r overlayModuleResolver) Resolve(importPath, basePath string) ([]byte, string, error) {
	resolved := filepath.Join(r.root, filepath.FromSlash(importPath)+".arb")
	resolved, err := filepath.Abs(resolved)
	if err != nil {
		return nil, "", fmt.Errorf("resolve import %q: %w", importPath, err)
	}
	resolved = filepath.Clean(resolved)
	if content, ok := r.overlays[resolved]; ok {
		return []byte(content), resolved, nil
	}
	source, err := os.ReadFile(resolved)
	if err != nil {
		return nil, "", fmt.Errorf("cannot resolve import %q: %w", importPath, err)
	}
	return source, resolved, nil
}

func compileWorkspaceDiagnostics(files map[string]*fileState) map[string][]map[string]any {
	out := make(map[string][]map[string]any, len(files))
	overlay := snapshotOverlays(files)
	moduleErrs := make(map[string]error)

	uris := make([]string, 0, len(files))
	for uri := range files {
		uris = append(uris, uri)
	}
	sort.Strings(uris)

	for _, uri := range uris {
		state := files[uri]
		if state == nil {
			continue
		}
		if path, ok := uriToPath(uri); ok {
			out[uri] = compileFileDiagnostics(path, state.content, overlay, moduleErrs)
			continue
		}
		out[uri] = compileUntitledDiagnostics(state.content)
	}
	return out
}

func snapshotOverlays(files map[string]*fileState) map[string]string {
	overlay := make(map[string]string, len(files))
	for uri, state := range files {
		path, ok := uriToPath(uri)
		if !ok || state == nil {
			continue
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		overlay[filepath.Clean(absPath)] = state.content
	}
	return overlay
}

func compileUntitledDiagnostics(source string) []map[string]any {
	prog, err := arbiter.Compile([]byte(source))
	var diags []map[string]any
	diags = append(diags, diagnosticsForOwnErrors("", err, true)...)
	if prog != nil {
		diags = append(diags, diagnosticsForWarnings("", prog.Warnings)...)
	}
	return diags
}

func compileFileDiagnostics(path, source string, overlay map[string]string, moduleErrs map[string]error) []map[string]any {
	resolver := overlayModuleResolver{
		root:     projectRoot(path),
		overlays: overlay,
	}
	prog, err := arbiter.CompileFileSource(path, []byte(source), arbiter.WithResolver(resolver))
	imports := extractImportSites(path, source, resolver)

	diags := diagnosticsForOwnErrors(path, err, len(imports) == 0)
	diags = filterImportedDeclarationDiagnostics(diags, imports)
	if prog != nil {
		diags = append(diags, diagnosticsForWarnings(path, prog.Warnings)...)
	}

	for _, site := range imports {
		if site.ResolveErr != nil {
			diags = append(diags, diagnosticAt(site.Line, site.Character, 1, site.ResolveErr.Error()))
			continue
		}
		if site.ResolvedPath == "" {
			continue
		}
		if importErr := compileModuleError(site.ResolvedPath, overlay, moduleErrs); importErr != nil {
			diags = append(diags, diagnosticAt(site.Line, site.Character, 1, fmt.Sprintf(`imported module %q has errors`, site.Path)))
		}
	}

	return diags
}

func filterImportedDeclarationDiagnostics(diags []map[string]any, imports []importSite) []map[string]any {
	if len(diags) == 0 || len(imports) == 0 {
		return diags
	}
	prefixes := make([]string, 0, len(imports)*4)
	for _, site := range imports {
		if site.Alias == "" {
			continue
		}
		prefixes = append(prefixes,
			"rule "+site.Alias+".",
			"expert rule "+site.Alias+".",
			"flag "+site.Alias+".",
			"strategy "+site.Alias+".",
		)
	}
	if len(prefixes) == 0 {
		return diags
	}
	out := diags[:0]
	for _, diag := range diags {
		message, _ := diag["message"].(string)
		skip := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(message, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, diag)
		}
	}
	return out
}

func compileModuleError(path string, overlay map[string]string, cache map[string]error) error {
	path = filepath.Clean(path)
	if err, ok := cache[path]; ok {
		return err
	}
	content, ok := overlay[path]
	if !ok {
		bytes, err := os.ReadFile(path)
		if err != nil {
			cache[path] = err
			return err
		}
		content = string(bytes)
	}
	resolver := overlayModuleResolver{
		root:     projectRoot(path),
		overlays: overlay,
	}
	_, err := arbiter.CompileFileSource(path, []byte(content), arbiter.WithResolver(resolver))
	cache[path] = err
	return err
}

func extractImportSites(path, source string, resolver arbiter.IncludeResolver) []importSite {
	parsed, err := arbiter.ParseSource([]byte(source))
	if err != nil {
		return nil
	}
	program, err := ir.Lower(parsed.Root, parsed.Source, parsed.Lang)
	if err != nil || program == nil {
		return nil
	}
	out := make([]importSite, 0, len(program.Imports))
	for _, imp := range program.Imports {
		alias := imp.Alias
		if alias == "" {
			parts := strings.Split(imp.Path, "/")
			alias = parts[len(parts)-1]
		}
		_, resolved, resolveErr := resolver.Resolve(imp.Path, filepath.Dir(path))
		out = append(out, importSite{
			Path:         imp.Path,
			Alias:        alias,
			Line:         int(imp.Span.StartRow),
			Character:    int(imp.Span.StartCol),
			ResolvedPath: resolved,
			ResolveErr:   resolveErr,
		})
	}
	return out
}

func diagnosticsForOwnErrors(path string, err error, includeGeneric bool) []map[string]any {
	if err == nil {
		return nil
	}
	var diags []map[string]any
	for _, item := range flattenErrors(err) {
		if item == nil {
			continue
		}
		var diag *arbiter.DiagnosticError
		if errors.As(item, &diag) {
			if diag.File != "" && path != "" && !samePath(diag.File, path) {
				continue
			}
			diags = append(diags, diagnosticAt(diag.Line-1, diag.Column-1, 1, diag.Message))
			continue
		}
		if !includeGeneric {
			continue
		}
		message := strings.TrimSpace(item.Error())
		if message == "" {
			continue
		}
		diags = append(diags, diagnosticAt(0, 0, 1, message))
	}
	return diags
}

func diagnosticsForWarnings(path string, warnings []arbiter.Diagnostic) []map[string]any {
	if len(warnings) == 0 {
		return nil
	}
	diags := make([]map[string]any, 0, len(warnings))
	for _, warning := range warnings {
		if warning.File != "" && path != "" && !samePath(warning.File, path) {
			continue
		}
		diags = append(diags, diagnosticAt(warning.Line-1, warning.Col-1, 2, warning.Message))
	}
	return diags
}

func diagnosticAt(line, col, severity int, message string) map[string]any {
	if line < 0 {
		line = 0
	}
	if col < 0 {
		col = 0
	}
	return map[string]any{
		"range": map[string]any{
			"start": map[string]any{"line": line, "character": col},
			"end":   map[string]any{"line": line, "character": col + 1},
		},
		"severity": severity,
		"source":   "arbiter",
		"message":  message,
	}
}

func flattenErrors(err error) []error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var out []error
		for _, item := range joined.Unwrap() {
			out = append(out, flattenErrors(item)...)
		}
		return out
	}
	return []error{err}
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

func projectRoot(path string) string {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return filepath.Dir(path)
	}
	dir := filepath.Dir(absPath)
	for {
		if _, err := os.Stat(filepath.Join(dir, "arbiter.toml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(absPath)
		}
		dir = parent
	}
}
