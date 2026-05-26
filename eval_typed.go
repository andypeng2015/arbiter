package arbiter

import (
	"m31labs.dev/arbiter/govern"
	"m31labs.dev/arbiter/vm"
)

// EvalTyped evaluates a compiled program against a typed struct.
func EvalTyped[T any](prog *Program, facts T, opts ...EvalOption) ([]vm.MatchedRule, error) {
	dc := DataFromStruct(facts, prog)
	return Eval(prog, dc, opts...)
}

// EvalGovernedTyped evaluates with governance against a typed struct.
func EvalGovernedTyped[T any](prog *Program, facts T, segments *govern.SegmentSet, ctx map[string]any, opts ...EvalOption) ([]vm.MatchedRule, *govern.Arbitrace, error) {
	dc := DataFromStruct(facts, prog)
	return EvalGoverned(prog, dc, segments, ctx, opts...)
}
