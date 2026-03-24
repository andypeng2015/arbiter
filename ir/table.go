package ir

// Table is one top-level table declaration.
type Table struct {
	Name    string
	Columns []TableColumn
	Rows    []TableRow
	Span    Span
}

// TableColumn is one column header in a table declaration.
type TableColumn struct {
	Name string
	Type FieldType
}

// TableRow is one data row in a table declaration.
type TableRow struct {
	Values []ExprID // one per column, references literal expressions
	Span   Span
}
