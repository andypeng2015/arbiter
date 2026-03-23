package main

import (
	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/explore"
	"github.com/odvcencio/arbiter/ir"
)

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
	kind string // "rule", "fact", "outcome", "segment", "strategy", "expert", "const", "flag", "worker", "arbiter"
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
