package main

import (
	"net/url"
	"strings"

	arbiter "m31labs.dev/arbiter"
	"m31labs.dev/arbiter/explore"
	"m31labs.dev/arbiter/ir"
)

// compileForDiagnostics compiles source appropriate to the URI.
// For file:// URIs it uses CompileFile so imports and input schema are resolved.
// For untitled/in-memory URIs it falls back to Compile (no import support).
// Returns the program (may be nil on error), any compile error, and any warnings.
func compileForDiagnostics(uri string, source []byte) (*arbiter.Program, error, []arbiter.Diagnostic) {
	if path, ok := uriToPath(uri); ok {
		prog, err := arbiter.CompileFile(path)
		if err != nil {
			return nil, err, nil
		}
		return prog, nil, prog.Warnings
	}
	// Fall back to in-memory compile for unsaved/untitled buffers.
	prog, err := arbiter.Compile(source)
	if err != nil {
		return nil, err, nil
	}
	return prog, nil, prog.Warnings
}

// uriToPath converts a file:// URI to an absolute filesystem path.
// Returns ("", false) for non-file URIs.
func uriToPath(uri string) (string, bool) {
	if !strings.HasPrefix(uri, "file://") {
		return "", false
	}
	u, err := url.Parse(uri)
	if err != nil {
		return "", false
	}
	path := u.Path
	if path == "" {
		return "", false
	}
	return path, true
}

// compileAndValidate compiles source in-memory (no import resolution).
func compileAndValidate(source []byte) error {
	_, err := arbiter.CompileFull(source)
	return err
}

func getSummary(source []byte) *explore.Summary {
	full, err := arbiter.CompileFull(source)
	if err != nil {
		return nil
	}
	return explore.BuildSummary(full.Program)
}

// symbolLocation holds a declaration's name and source position.
type symbolLocation struct {
	name string
	kind string // "rule", "fact", "outcome", "segment", "strategy", "expert", "const", "flag", "worker", "arbiter", "table"
	span ir.Span
}

// getSymbols extracts all named declaration locations from compiled source.
func getSymbols(source []byte) []symbolLocation {
	full, err := arbiter.CompileFull(source)
	if err != nil || full == nil || full.Program == nil {
		return nil
	}
	p := full.Program
	var syms []symbolLocation

	for _, c := range p.Consts {
		syms = append(syms, symbolLocation{name: c.Name, kind: "const", span: c.Span})
	}
	for _, f := range p.FactSchemas {
		syms = append(syms, symbolLocation{name: f.Name, kind: "fact", span: f.Span})
	}
	for _, o := range p.OutcomeSchemas {
		syms = append(syms, symbolLocation{name: o.Name, kind: "outcome", span: o.Span})
	}
	for _, s := range p.Segments {
		syms = append(syms, symbolLocation{name: s.Name, kind: "segment", span: s.Span})
	}
	for _, r := range p.Rules {
		syms = append(syms, symbolLocation{name: r.Name, kind: "rule", span: r.Span})
	}
	for _, s := range p.Strategies {
		syms = append(syms, symbolLocation{name: s.Name, kind: "strategy", span: s.Span})
	}
	for _, f := range p.Flags {
		syms = append(syms, symbolLocation{name: f.Name, kind: "flag", span: f.Span})
	}
	for _, e := range p.Expert {
		syms = append(syms, symbolLocation{name: e.Name, kind: "expert", span: e.Span})
	}
	for _, w := range p.Workers {
		syms = append(syms, symbolLocation{name: w.Name, kind: "worker", span: w.Span})
	}
	for _, a := range p.Arbiters {
		syms = append(syms, symbolLocation{name: a.Name, kind: "arbiter", span: a.Span})
	}
	for _, t := range p.Tables {
		syms = append(syms, symbolLocation{name: t.Name, kind: "table", span: t.Span})
	}
	return syms
}

// findDefinition returns the span of the declaration with the given name.
func findDefinition(source []byte, name string) *ir.Span {
	for _, sym := range getSymbols(source) {
		if sym.name == name {
			s := sym.span
			return &s
		}
	}
	return nil
}

// findReferences returns all spans where a name is referenced.
// Currently finds: requires/excludes references, segment references,
// and action name references in rules.
func findReferences(source []byte, name string) []ir.Span {
	full, err := arbiter.CompileFull(source)
	if err != nil || full == nil || full.Program == nil {
		return nil
	}
	p := full.Program
	var refs []ir.Span

	// Declaration site.
	if defSpan := findDefinition(source, name); defSpan != nil {
		refs = append(refs, *defSpan)
	}

	// References in rules: requires, excludes, segment.
	for _, r := range p.Rules {
		if r.Segment == name {
			refs = append(refs, r.Span)
		}
		for _, req := range r.Prereqs {
			if req == name {
				refs = append(refs, r.Span)
			}
		}
		for _, exc := range r.Excludes {
			if exc == name {
				refs = append(refs, r.Span)
			}
		}
	}
	// References in expert rules.
	for _, e := range p.Expert {
		if e.Segment == name {
			refs = append(refs, e.Span)
		}
		for _, req := range e.Prereqs {
			if req == name {
				refs = append(refs, e.Span)
			}
		}
		for _, exc := range e.Excludes {
			if exc == name {
				refs = append(refs, e.Span)
			}
		}
	}
	// References in strategy candidates.
	for _, s := range p.Strategies {
		for _, c := range s.Candidates {
			if c.Segment == name {
				refs = append(refs, s.Span)
			}
		}
	}
	return refs
}
