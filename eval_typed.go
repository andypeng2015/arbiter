package arbiter

import (
	"github.com/odvcencio/arbiter/compiler"
	"github.com/odvcencio/arbiter/govern"
	"github.com/odvcencio/arbiter/vm"
)

// EvalTyped evaluates a compiled ruleset against a typed struct.
func EvalTyped[T any](rs *compiler.CompiledRuleset, facts T) ([]vm.MatchedRule, error) {
	dc := DataFromStruct(facts, rs)
	return Eval(rs, dc)
}

// EvalGovernedTyped evaluates with governance against a typed struct.
func EvalGovernedTyped[T any](rs *compiler.CompiledRuleset, facts T, segments *govern.SegmentSet, ctx map[string]any) ([]vm.MatchedRule, *govern.Trace, error) {
	dc := DataFromStruct(facts, rs)
	return EvalGoverned(rs, dc, segments, ctx)
}
