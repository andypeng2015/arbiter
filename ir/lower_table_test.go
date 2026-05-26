package ir_test

import (
	"testing"

	"m31labs.dev/arbiter/ir"
)

func TestLowerTable(t *testing.T) {
	program := lowerSource(t, `table ladder {
    height: number | bitrate: string | preset: string
    1080           | "6500k"         | "p3"
    720            | "4500k"         | "p2"
}`)

	if got := len(program.Tables); got != 1 {
		t.Fatalf("len(Tables) = %d, want 1", got)
	}
	tbl := program.Tables[0]
	if tbl.Name != "ladder" {
		t.Fatalf("Table.Name = %q, want ladder", tbl.Name)
	}
	if got := len(tbl.Columns); got != 3 {
		t.Fatalf("len(Columns) = %d, want 3", got)
	}
	if tbl.Columns[0].Name != "height" || tbl.Columns[0].Type.Base != "number" {
		t.Fatalf("Columns[0] = %+v, want height:number", tbl.Columns[0])
	}
	if tbl.Columns[1].Name != "bitrate" || tbl.Columns[1].Type.Base != "string" {
		t.Fatalf("Columns[1] = %+v, want bitrate:string", tbl.Columns[1])
	}
	if tbl.Columns[2].Name != "preset" || tbl.Columns[2].Type.Base != "string" {
		t.Fatalf("Columns[2] = %+v, want preset:string", tbl.Columns[2])
	}

	if got := len(tbl.Rows); got != 2 {
		t.Fatalf("len(Rows) = %d, want 2", got)
	}

	// Row 0: 1080 | "6500k" | "p3"
	row0 := tbl.Rows[0]
	if got := len(row0.Values); got != 3 {
		t.Fatalf("len(Rows[0].Values) = %d, want 3", got)
	}
	e0 := program.Expr(row0.Values[0])
	if e0 == nil || e0.Kind != ir.ExprNumberLit || e0.Number != 1080 {
		t.Fatalf("Rows[0][0] = %+v, want 1080", e0)
	}
	e1 := program.Expr(row0.Values[1])
	if e1 == nil || e1.Kind != ir.ExprStringLit || e1.String != "6500k" {
		t.Fatalf("Rows[0][1] = %+v, want 6500k", e1)
	}
	e2 := program.Expr(row0.Values[2])
	if e2 == nil || e2.Kind != ir.ExprStringLit || e2.String != "p3" {
		t.Fatalf("Rows[0][2] = %+v, want p3", e2)
	}

	// Row 1: 720 | "4500k" | "p2"
	row1 := tbl.Rows[1]
	e3 := program.Expr(row1.Values[0])
	if e3 == nil || e3.Kind != ir.ExprNumberLit || e3.Number != 720 {
		t.Fatalf("Rows[1][0] = %+v, want 720", e3)
	}

	// Verify TableByName works
	found, ok := program.TableByName("ladder")
	if !ok {
		t.Fatal("TableByName(ladder) returned false")
	}
	if found.Name != "ladder" {
		t.Fatalf("TableByName(ladder).Name = %q", found.Name)
	}
	_, ok = program.TableByName("nonexistent")
	if ok {
		t.Fatal("TableByName(nonexistent) returned true")
	}
}

func TestLowerLookupExpr(t *testing.T) {
	program := lowerSource(t, `table rates {
    tier: string | rate: number
    "gold"       | 50
    "silver"     | 30
}

rule ApplyRate {
    when { true }
    then Charge {
        let row = lookup rates where tier == user.tier order by rate desc else { tier: "default", rate: 0 }
        amount: row.rate
    }
}`)

	if got := len(program.Rules); got != 1 {
		t.Fatalf("len(Rules) = %d, want 1", got)
	}
	rule := program.Rules[0]
	action := rule.Action

	if got := len(action.Lets); got != 1 {
		t.Fatalf("len(Action.Lets) = %d, want 1", got)
	}
	letBinding := action.Lets[0]
	if letBinding.Name != "row" {
		t.Fatalf("let name = %q, want row", letBinding.Name)
	}

	expr := program.Expr(letBinding.Value)
	if expr == nil {
		t.Fatal("let value expr is nil")
	}
	if expr.Kind != ir.ExprLookup {
		t.Fatalf("let value kind = %q, want %q", expr.Kind, ir.ExprLookup)
	}
	if expr.TableName != "rates" {
		t.Fatalf("lookup TableName = %q, want rates", expr.TableName)
	}
	if expr.Where == 0 {
		t.Fatal("lookup Where = 0, expected a where expression")
	}
	if expr.SortCol != "rate" {
		t.Fatalf("lookup SortCol = %q, want rate", expr.SortCol)
	}
	if !expr.SortDesc {
		t.Fatal("lookup SortDesc = false, want true")
	}
	if len(expr.ElseKeys) != 2 {
		t.Fatalf("len(ElseKeys) = %d, want 2", len(expr.ElseKeys))
	}
	if expr.ElseKeys[0] != "tier" || expr.ElseKeys[1] != "rate" {
		t.Fatalf("ElseKeys = %v, want [tier rate]", expr.ElseKeys)
	}
	if len(expr.ElseVals) != 2 {
		t.Fatalf("len(ElseVals) = %d, want 2", len(expr.ElseVals))
	}
	elseVal0 := program.Expr(expr.ElseVals[0])
	if elseVal0 == nil || elseVal0.Kind != ir.ExprStringLit || elseVal0.String != "default" {
		t.Fatalf("ElseVals[0] = %+v, want default", elseVal0)
	}
	elseVal1 := program.Expr(expr.ElseVals[1])
	if elseVal1 == nil || elseVal1.Kind != ir.ExprNumberLit || elseVal1.Number != 0 {
		t.Fatalf("ElseVals[1] = %+v, want 0", elseVal1)
	}
}

func TestLowerLookupMinimal(t *testing.T) {
	program := lowerSource(t, `table t {
    x: number
    1
    2
}

rule R {
    when { true }
    then A {
        let row = lookup t
        val: row.x
    }
}`)

	rule := program.Rules[0]
	if got := len(rule.Action.Lets); got != 1 {
		t.Fatalf("len(Action.Lets) = %d, want 1", got)
	}
	expr := program.Expr(rule.Action.Lets[0].Value)
	if expr == nil || expr.Kind != ir.ExprLookup {
		t.Fatalf("let value = %+v, want ExprLookup", expr)
	}
	if expr.TableName != "t" {
		t.Fatalf("lookup TableName = %q, want t", expr.TableName)
	}
	if expr.Where != 0 {
		t.Fatalf("lookup Where = %d, want 0 (no where clause)", expr.Where)
	}
	if expr.SortCol != "" {
		t.Fatalf("lookup SortCol = %q, want empty", expr.SortCol)
	}
}

func TestLowerLetInAction(t *testing.T) {
	program := lowerSource(t, `rule R {
    when { true }
    then DoSomething {
        let x = 42
        let y = "hello"
        amount: x
        label: y
    }
}`)

	if got := len(program.Rules); got != 1 {
		t.Fatalf("len(Rules) = %d, want 1", got)
	}
	rule := program.Rules[0]
	action := rule.Action

	if got := len(action.Lets); got != 2 {
		t.Fatalf("len(Action.Lets) = %d, want 2", got)
	}
	if action.Lets[0].Name != "x" {
		t.Fatalf("action.Lets[0].Name = %q, want x", action.Lets[0].Name)
	}
	if action.Lets[1].Name != "y" {
		t.Fatalf("action.Lets[1].Name = %q, want y", action.Lets[1].Name)
	}

	// Verify let values are correct expressions
	xExpr := program.Expr(action.Lets[0].Value)
	if xExpr == nil || xExpr.Kind != ir.ExprNumberLit || xExpr.Number != 42 {
		t.Fatalf("let x value = %+v, want 42", xExpr)
	}
	yExpr := program.Expr(action.Lets[1].Value)
	if yExpr == nil || yExpr.Kind != ir.ExprStringLit || yExpr.String != "hello" {
		t.Fatalf("let y value = %+v, want hello", yExpr)
	}

	// Verify params are present and use local refs
	if got := len(action.Params); got != 2 {
		t.Fatalf("len(Action.Params) = %d, want 2", got)
	}
	if action.Params[0].Key != "amount" {
		t.Fatalf("action.Params[0].Key = %q, want amount", action.Params[0].Key)
	}
	amountExpr := program.Expr(action.Params[0].Value)
	if amountExpr == nil || amountExpr.Kind != ir.ExprLocalRef || amountExpr.Name != "x" {
		t.Fatalf("amount value = %+v, want local ref x", amountExpr)
	}
}

func TestLowerLetInOtherwiseBlock(t *testing.T) {
	program := lowerSource(t, `rule R {
    when { true }
    then Accept {}
    otherwise Reject {
        let reason = "denied"
        msg: reason
    }
}`)

	rule := program.Rules[0]
	if rule.Fallback == nil {
		t.Fatal("rule.Fallback = nil")
	}
	if got := len(rule.Fallback.Lets); got != 1 {
		t.Fatalf("len(Fallback.Lets) = %d, want 1", got)
	}
	if rule.Fallback.Lets[0].Name != "reason" {
		t.Fatalf("Fallback.Lets[0].Name = %q, want reason", rule.Fallback.Lets[0].Name)
	}
}
