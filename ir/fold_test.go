package ir

import "testing"

func TestFoldConstantsArithmetic(t *testing.T) {
	p := &Program{
		Exprs: []Expr{
			{Kind: ExprNumberLit, Number: 10},     // 0
			{Kind: ExprNumberLit, Number: 3},       // 1
			{Kind: ExprBinary, BinaryOp: BinaryAdd, Left: 0, Right: 1}, // 2: 10 + 3
			{Kind: ExprBinary, BinaryOp: BinaryMul, Left: 0, Right: 1}, // 3: 10 * 3
			{Kind: ExprBinary, BinaryOp: BinaryDiv, Left: 0, Right: 1}, // 4: 10 / 3
		},
	}

	FoldConstants(p)

	if p.Exprs[2].Kind != ExprNumberLit || p.Exprs[2].Number != 13 {
		t.Errorf("10+3: expected 13, got %v %v", p.Exprs[2].Kind, p.Exprs[2].Number)
	}
	if p.Exprs[3].Kind != ExprNumberLit || p.Exprs[3].Number != 30 {
		t.Errorf("10*3: expected 30, got %v %v", p.Exprs[3].Kind, p.Exprs[3].Number)
	}
	if p.Exprs[4].Kind != ExprNumberLit {
		t.Errorf("10/3: expected number_lit, got %v", p.Exprs[4].Kind)
	}
}

func TestFoldConstantsBooleanShortCircuit(t *testing.T) {
	p := &Program{
		Exprs: []Expr{
			{Kind: ExprBoolLit, Bool: false},       // 0
			{Kind: ExprVarRef, Path: "x"},           // 1
			{Kind: ExprBinary, BinaryOp: BinaryAnd, Left: 0, Right: 1}, // 2: false and x → false
			{Kind: ExprBoolLit, Bool: true},         // 3
			{Kind: ExprBinary, BinaryOp: BinaryOr, Left: 3, Right: 1},  // 4: true or x → true
			{Kind: ExprBinary, BinaryOp: BinaryAnd, Left: 3, Right: 1}, // 5: true and x → x
		},
	}

	FoldConstants(p)

	if p.Exprs[2].Kind != ExprBoolLit || p.Exprs[2].Bool {
		t.Errorf("false and x: expected false, got %v %v", p.Exprs[2].Kind, p.Exprs[2].Bool)
	}
	if p.Exprs[4].Kind != ExprBoolLit || !p.Exprs[4].Bool {
		t.Errorf("true or x: expected true, got %v %v", p.Exprs[4].Kind, p.Exprs[4].Bool)
	}
	if p.Exprs[5].Kind != ExprVarRef || p.Exprs[5].Path != "x" {
		t.Errorf("true and x: expected var_ref x, got %v %v", p.Exprs[5].Kind, p.Exprs[5].Path)
	}
}

func TestFoldConstantsNotLiteral(t *testing.T) {
	p := &Program{
		Exprs: []Expr{
			{Kind: ExprBoolLit, Bool: true},         // 0
			{Kind: ExprUnary, UnaryOp: UnaryNot, Operand: 0}, // 1: not true → false
		},
	}

	FoldConstants(p)

	if p.Exprs[1].Kind != ExprBoolLit || p.Exprs[1].Bool {
		t.Errorf("not true: expected false, got %v %v", p.Exprs[1].Kind, p.Exprs[1].Bool)
	}
}

func TestFoldConstantsInlinesConstRef(t *testing.T) {
	p := &Program{
		Consts: []Const{
			{Name: "LIMIT", Value: 0},
		},
		Exprs: []Expr{
			{Kind: ExprNumberLit, Number: 42},           // 0: the const value
			{Kind: ExprConstRef, Name: "LIMIT"},          // 1: ref to LIMIT
			{Kind: ExprBinary, BinaryOp: BinaryAdd, Left: 1, Right: 1}, // 2: LIMIT + LIMIT
		},
	}
	p.RebuildIndexes()

	FoldConstants(p)

	// Const ref should be inlined to literal.
	if p.Exprs[1].Kind != ExprNumberLit || p.Exprs[1].Number != 42 {
		t.Errorf("LIMIT ref: expected number 42, got %v %v", p.Exprs[1].Kind, p.Exprs[1].Number)
	}
	// LIMIT + LIMIT should fold to 84.
	if p.Exprs[2].Kind != ExprNumberLit || p.Exprs[2].Number != 84 {
		t.Errorf("LIMIT+LIMIT: expected 84, got %v %v", p.Exprs[2].Kind, p.Exprs[2].Number)
	}
}

func TestFoldConstantsDivByZeroNotFolded(t *testing.T) {
	p := &Program{
		Exprs: []Expr{
			{Kind: ExprNumberLit, Number: 10},
			{Kind: ExprNumberLit, Number: 0},
			{Kind: ExprBinary, BinaryOp: BinaryDiv, Left: 0, Right: 1},
		},
	}

	FoldConstants(p)

	// Division by zero should not be folded — leave for runtime.
	if p.Exprs[2].Kind != ExprBinary {
		t.Errorf("10/0: expected to stay as binary, got %v", p.Exprs[2].Kind)
	}
}
