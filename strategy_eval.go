package arbiter

import (
	"fmt"

	"github.com/odvcencio/arbiter/overrides"
	"github.com/odvcencio/arbiter/strategy"
)

// CompileStrategies compiles all strategy declarations from raw .arb source.
//
// Deprecated: use Compile and access prog.Strategies instead. Will be removed in v2.0.0.
func CompileStrategies(source []byte) (*strategy.Strategies, error) {
	full, err := CompileFull(source)
	if err != nil {
		return nil, err
	}
	return full.Strategies, nil
}

// CompileStrategiesParsed compiles all strategy declarations from parsed source.
//
// Deprecated: use Compile and access prog.Strategies instead. Will be removed in v2.0.0.
func CompileStrategiesParsed(parsed *ParsedSource) (*strategy.Strategies, error) {
	full, err := CompileFullParsed(parsed)
	if err != nil {
		return nil, err
	}
	return full.Strategies, nil
}

// CompileStrategiesFile resolves includes and compiles all strategy declarations.
//
// Deprecated: use CompileFile and access prog.Strategies instead. Will be removed in v2.0.0.
func CompileStrategiesFile(path string) (*strategy.Strategies, error) {
	full, err := CompileFullFile(path)
	if err != nil {
		return nil, err
	}
	return full.Strategies, nil
}

// EvalStrategy evaluates one compiled strategy against the given request context.
//
// Deprecated: use Compile and call prog.Strategies.Evaluate directly. Will be removed in v2.0.0.
func EvalStrategy(compiled *CompileResult, name string, ctx map[string]any) (strategy.Result, error) {
	return EvalStrategyWithOverrides(compiled, name, ctx, "", nil)
}

// EvalStrategyWithOverrides evaluates one compiled strategy while applying
// runtime candidate overrides.
//
// Deprecated: use Compile and call prog.Strategies.EvaluateWithOverrides directly. Will be removed in v2.0.0.
func EvalStrategyWithOverrides(compiled *CompileResult, name string, ctx map[string]any, bundleID string, view overrides.View) (strategy.Result, error) {
	if compiled == nil {
		return strategy.Result{}, fmt.Errorf("nil compiled program")
	}
	if compiled.Strategies == nil {
		return strategy.Result{}, fmt.Errorf("nil compiled strategies")
	}
	return compiled.Strategies.EvaluateWithOverrides(name, ctx, bundleID, view)
}
