package arbiter

import (
	"github.com/odvcencio/arbiter/govern"
	"github.com/odvcencio/arbiter/vm"
)

// EvalTyped evaluates a compiled program against a typed struct.
func EvalTyped[T any](prog *Program, facts T, opts ...EvalOption) ([]vm.MatchedRule, error) {
	dc := DataFromStruct(facts, prog)
	return Eval(prog, dc, opts...)
}

// EvalGovernedTyped evaluates with governance against a typed struct.
func EvalGovernedTyped[T any](prog *Program, facts T, segments *govern.SegmentSet, ctx map[string]any, opts ...EvalOption) ([]vm.MatchedRule, *govern.Trace, error) {
	dc := DataFromStruct(facts, prog)
	return EvalGoverned(prog, dc, segments, ctx, opts...)
}
