package arbiter

import (
	"sync"

	"m31labs.dev/arbiter/compiler"
	"m31labs.dev/arbiter/govern"
	"m31labs.dev/arbiter/ir"
	"m31labs.dev/arbiter/strategy"
	"m31labs.dev/arbiter/vm"
)

// DiagnosticSeverity indicates the severity level of a Diagnostic.
type DiagnosticSeverity int

const (
	// DiagWarning is a non-fatal warning diagnostic.
	DiagWarning DiagnosticSeverity = iota
	// DiagInfo is an informational diagnostic.
	DiagInfo
)

// Diagnostic is a non-fatal compiler message with file and position data.
type Diagnostic struct {
	Severity DiagnosticSeverity
	Message  string
	File     string
	Line     int
	Col      int
}

// Program is the compiled output of an Arbiter source. It contains all
// artifacts needed for evaluation: bytecode, segments, strategies, etc.
type Program struct {
	Ruleset    *compiler.CompiledRuleset
	Segments   *govern.SegmentSet
	Strategies *strategy.Strategies
	Expert     interface{} // *expert.Program when expert declarations exist; nil otherwise
	IR         *ir.Program
	Workers    map[string]WorkerDeclaration
	Arbiters   []ArbiterDeclaration
	Input      *ir.InputSchema
	Warnings   []Diagnostic   // non-fatal diagnostics collected during compilation
	pool       *vm.StringPool // internal, sealed from public API
	poolOnce   sync.Once
}

// stringPool returns the string pool, lazily initializing from the ruleset if needed.
func (p *Program) stringPool() *vm.StringPool {
	if p == nil {
		return nil
	}
	p.poolOnce.Do(func() {
		if p.Ruleset != nil {
			p.pool = vm.NewStringPool(p.Ruleset.Constants.Strings())
		}
	})
	return p.pool
}

// Option configures optional Compile behavior.
type Option func(*compileOptions)

type compileOptions struct {
	manifest *string
	resolver IncludeResolver
}

// WithManifest sets the manifest path for module resolution.
func WithManifest(path string) Option {
	return func(o *compileOptions) { o.manifest = &path }
}

// WithResolver sets the include resolver for import resolution.
func WithResolver(r IncludeResolver) Option {
	return func(o *compileOptions) { o.resolver = r }
}

// toCompileResult converts a Program to the legacy CompileResult type.
func (p *Program) toCompileResult() *CompileResult {
	if p == nil {
		return nil
	}
	return &CompileResult{
		Ruleset:    p.Ruleset,
		Segments:   p.Segments,
		Strategies: p.Strategies,
		Workers:    p.Workers,
		Arbiters:   p.Arbiters,
		Program:    p.IR,
	}
}

// programFromResult converts a CompileResult to a Program.
func programFromResult(cr *CompileResult) *Program {
	if cr == nil {
		return nil
	}
	return &Program{
		Ruleset:    cr.Ruleset,
		Segments:   cr.Segments,
		Strategies: cr.Strategies,
		IR:         cr.Program,
		Workers:    cr.Workers,
		Arbiters:   cr.Arbiters,
	}
}
