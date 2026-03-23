//go:build js && wasm

package main

import (
	"encoding/json"
	"syscall/js"

	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/strategy"
	"github.com/odvcencio/arbiter/vm"
)

func main() {
	js.Global().Set("arbiterCompile", js.FuncOf(compile))
	js.Global().Set("arbiterEval", js.FuncOf(eval))
	js.Global().Set("arbiterEvalGoverned", js.FuncOf(evalGoverned))
	js.Global().Set("arbiterEvalStrategy", js.FuncOf(evalStrategy))

	// Block forever — WASM module stays alive.
	select {}
}

// compiled holds the last compiled bundle for reuse.
var compiled *arbiter.CompileResult

func compile(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return jsError("compile requires .arb source string")
	}
	source := args[0].String()
	full, err := arbiter.CompileFull([]byte(source))
	if err != nil {
		return jsError(err.Error())
	}
	compiled = full
	return jsResult(map[string]any{
		"rules":      len(full.Ruleset.Rules),
		"strategies": full.Strategies.Count(),
	})
}

func eval(_ js.Value, args []js.Value) any {
	if compiled == nil {
		return jsError("no compiled bundle — call arbiterCompile first")
	}
	if len(args) < 1 {
		return jsError("eval requires JSON context string")
	}
	dc, err := arbiter.DataFromJSON(args[0].String(), compiled.Ruleset)
	if err != nil {
		return jsError(err.Error())
	}
	matched, err := arbiter.Eval(compiled.Ruleset, dc)
	if err != nil {
		return jsError(err.Error())
	}
	return jsMatchedRules(matched)
}

func evalGoverned(_ js.Value, args []js.Value) any {
	if compiled == nil {
		return jsError("no compiled bundle — call arbiterCompile first")
	}
	if len(args) < 1 {
		return jsError("evalGoverned requires JSON context string")
	}
	var ctx map[string]any
	if err := json.Unmarshal([]byte(args[0].String()), &ctx); err != nil {
		return jsError(err.Error())
	}
	dc, err := arbiter.DataFromJSON(args[0].String(), compiled.Ruleset)
	if err != nil {
		return jsError(err.Error())
	}
	matched, _, err := arbiter.EvalGoverned(compiled.Ruleset, dc, compiled.Segments, ctx)
	if err != nil {
		return jsError(err.Error())
	}
	return jsMatchedRules(matched)
}

func evalStrategy(_ js.Value, args []js.Value) any {
	if compiled == nil {
		return jsError("no compiled bundle — call arbiterCompile first")
	}
	if len(args) < 2 {
		return jsError("evalStrategy requires strategy name and JSON context string")
	}
	name := args[0].String()
	var ctx map[string]any
	if err := json.Unmarshal([]byte(args[1].String()), &ctx); err != nil {
		return jsError(err.Error())
	}
	result, err := arbiter.EvalStrategy(compiled, name, ctx)
	if err != nil {
		return jsError(err.Error())
	}
	return jsStrategyResult(result)
}

func jsError(msg string) any {
	return map[string]any{"error": msg}
}

func jsResult(data map[string]any) any {
	blob, _ := json.Marshal(data)
	return js.Global().Get("JSON").Call("parse", string(blob))
}

func jsMatchedRules(matched []vm.MatchedRule) any {
	results := make([]map[string]any, len(matched))
	for i, m := range matched {
		results[i] = map[string]any{
			"name":   m.Name,
			"action": m.Action,
			"params": m.Params,
		}
	}
	blob, _ := json.Marshal(results)
	return js.Global().Get("JSON").Call("parse", string(blob))
}

func jsStrategyResult(result strategy.Result) any {
	data := map[string]any{
		"strategy": result.Strategy,
		"outcome":  result.Outcome,
		"selected": result.Selected,
		"params":   result.Params,
	}
	blob, _ := json.Marshal(data)
	return js.Global().Get("JSON").Call("parse", string(blob))
}
