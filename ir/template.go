package ir

import "fmt"

// InlineTemplates resolves every template call by cloning the called template's
// body into the program's expression arena and substituting each parameter
// placeholder (ExprParamRef) with the corresponding argument expression. It runs
// after module merge, so cross-module template calls resolve here.
//
// A call is a template call when its FuncName matches a declared template;
// other calls (builtins like abs/round) are left untouched for the validator.
// Nested template calls are resolved recursively during cloning; a self- or
// mutually-recursive template is rejected via a name stack.
func InlineTemplates(program *Program) error {
	if program == nil || len(program.Templates) == 0 {
		return nil
	}
	byName := make(map[string]*Template, len(program.Templates))
	for i := range program.Templates {
		byName[program.Templates[i].Name] = &program.Templates[i]
	}
	r := &templateInliner{program: program, byName: byName, stack: map[string]bool{}}

	// Visit every call node in the arena. Each is its own ExprID and is
	// referenced by its parent by ID, so overwriting the slot with the inlined
	// body root redirects the parent without walking the parent set.
	n := len(program.Exprs)
	for id := 0; id < n; id++ {
		if program.Exprs[id].Kind != ExprBuiltinCall {
			continue
		}
		if _, ok := byName[program.Exprs[id].FuncName]; !ok {
			continue
		}
		span := program.Exprs[id].Span
		newID := r.resolve(ExprID(id), nil)
		if r.err != nil {
			return r.err
		}
		out := program.Exprs[newID]
		out.Span = span
		program.Exprs[id] = out
	}
	return r.err
}

type templateInliner struct {
	program *Program
	byName  map[string]*Template
	stack   map[string]bool // templates currently being inlined (recursion guard)
	err     error
}

func (r *templateInliner) appendExpr(e Expr) ExprID {
	r.program.Exprs = append(r.program.Exprs, e)
	return ExprID(len(r.program.Exprs) - 1)
}

// resolve deep-copies srcID into the arena, inlining template calls and
// substituting bound parameters. argByParam holds the parameter→argument
// bindings of the template currently being inlined (nil at the top level).
func (r *templateInliner) resolve(srcID ExprID, argByParam map[string]ExprID) ExprID {
	if r.err != nil {
		return srcID
	}
	src := r.program.Exprs[srcID] // value copy: stable across slice growth

	switch src.Kind {
	case ExprParamRef:
		if argByParam != nil {
			if argID, ok := argByParam[src.Name]; ok {
				return argID
			}
		}
		// Unbound here: preserve the placeholder for an enclosing template to
		// substitute when *it* is inlined.
		return r.appendExpr(src)

	case ExprBuiltinCall:
		tpl, ok := r.byName[src.FuncName]
		if !ok {
			// A real builtin — clone with resolved arguments.
			out := src
			out.Args = r.resolveSlice(src.Args, argByParam)
			return r.appendExpr(out)
		}
		if len(src.Args) != len(tpl.Params) {
			r.err = fmt.Errorf("template %q expects %d argument(s), got %d", tpl.Name, len(tpl.Params), len(src.Args))
			return r.appendExpr(Expr{Kind: ExprNullLit, Span: src.Span})
		}
		if r.stack[tpl.Name] {
			r.err = fmt.Errorf("recursive template %q is not allowed", tpl.Name)
			return r.appendExpr(Expr{Kind: ExprNullLit, Span: src.Span})
		}
		// Resolve arguments in the current context, then inline the body.
		bindings := make(map[string]ExprID, len(tpl.Params))
		for i, p := range tpl.Params {
			bindings[p] = r.resolve(src.Args[i], argByParam)
		}
		r.stack[tpl.Name] = true
		root := r.resolve(tpl.Body, bindings)
		delete(r.stack, tpl.Name)
		return root

	default:
		return r.appendExpr(r.cloneChildren(src, argByParam))
	}
}

// cloneChildren returns a copy of src with its child ExprIDs resolved. It clones
// only the fields each kind actually uses (the same set offsetExprIDs shifts),
// so unused-but-zero fields never trigger spurious cloning, and the Where
// sentinel (0 = no clause) is preserved.
func (r *templateInliner) cloneChildren(src Expr, argByParam map[string]ExprID) Expr {
	out := src
	switch src.Kind {
	case ExprListLit:
		out.Elems = r.resolveSlice(src.Elems, argByParam)
	case ExprBinary:
		out.Left = r.resolve(src.Left, argByParam)
		out.Right = r.resolve(src.Right, argByParam)
	case ExprUnary:
		out.Operand = r.resolve(src.Operand, argByParam)
	case ExprBetween:
		out.Value = r.resolve(src.Value, argByParam)
		out.Low = r.resolve(src.Low, argByParam)
		out.High = r.resolve(src.High, argByParam)
	case ExprQuantifier:
		out.Collection = r.resolve(src.Collection, argByParam)
		out.Body = r.resolve(src.Body, argByParam)
	case ExprAggregate:
		out.Collection = r.resolve(src.Collection, argByParam)
		if src.HasValueExpr {
			out.ValueExpr = r.resolve(src.ValueExpr, argByParam)
		}
	case ExprLookup:
		if src.Where != 0 {
			out.Where = r.resolve(src.Where, argByParam)
		}
		out.ElseVals = r.resolveSlice(src.ElseVals, argByParam)
	}
	return out
}

func (r *templateInliner) resolveSlice(ids []ExprID, argByParam map[string]ExprID) []ExprID {
	if len(ids) == 0 {
		return nil
	}
	out := make([]ExprID, len(ids))
	for i, id := range ids {
		out[i] = r.resolve(id, argByParam)
	}
	return out
}
