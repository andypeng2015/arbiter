package arbiter

import (
	"testing"
)

// flatStruct exercises simple flat arb tags.
type flatStruct struct {
	Name   string  `arb:"name"`
	Age    int     `arb:"age"`
	Score  float64 `arb:"score"`
	Active bool    `arb:"active"`
}

// nestedStruct exercises dot-notation arb tags.
type nestedStruct struct {
	TaskType   string `arb:"task.type"`
	TaskPrio   int    `arb:"task.priority"`
	UserName   string `arb:"user.name"`
}

// noTagStruct has no arb tags — fields should be skipped.
type noTagStruct struct {
	Ignored string
}

// mixedStruct has some tagged, some untagged fields.
type mixedStruct struct {
	Tagged   string `arb:"label"`
	Untagged string
}

// sliceStruct exercises slice field support.
type sliceStruct struct {
	Tags []string `arb:"tags"`
}

func TestDataFromStruct_FlatFields(t *testing.T) {
	s := flatStruct{
		Name:   "alice",
		Age:    30,
		Score:  9.5,
		Active: true,
	}

	rs, err := Compile([]byte(`
rule Check {
    when { name != "" }
    then Match {}
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	dc := DataFromStruct(s, rs)

	v := dc.Get("name")
	if v.IsNull() {
		t.Error("name: expected non-null value")
	}

	v = dc.Get("age")
	if v.IsNull() {
		t.Error("age: expected non-null value")
	}

	v = dc.Get("score")
	if v.IsNull() {
		t.Error("score: expected non-null value")
	}

	v = dc.Get("active")
	if v.IsNull() {
		t.Error("active: expected non-null value")
	}
	if !v.AsBool() {
		t.Error("active: expected true")
	}
}

func TestDataFromStruct_DotNotation(t *testing.T) {
	s := nestedStruct{
		TaskType: "critical",
		TaskPrio: 1,
		UserName: "bob",
	}

	rs, err := Compile([]byte(`
rule Check {
    when { task.type != "" }
    then Match {}
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	dc := DataFromStruct(s, rs)

	v := dc.Get("task.type")
	if v.IsNull() {
		t.Error("task.type: expected non-null value")
	}

	v = dc.Get("task.priority")
	if v.IsNull() {
		t.Error("task.priority: expected non-null value")
	}

	v = dc.Get("user.name")
	if v.IsNull() {
		t.Error("user.name: expected non-null value")
	}
}

func TestDataFromStruct_PointerInput(t *testing.T) {
	s := &flatStruct{
		Name:   "charlie",
		Active: false,
	}

	rs, err := Compile([]byte(`
rule Check {
    when { name != "" }
    then Match {}
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	dc := DataFromStruct(s, rs)

	v := dc.Get("name")
	if v.IsNull() {
		t.Error("name via pointer: expected non-null value")
	}
}

func TestDataFromStruct_NoArb_TagsSkipped(t *testing.T) {
	s := noTagStruct{Ignored: "value"}

	rs, err := Compile([]byte(`
rule Check {
    when { x != "" }
    then Match {}
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	dc := DataFromStruct(s, rs)

	v := dc.Get("Ignored")
	if !v.IsNull() {
		t.Error("untagged field should produce null for key 'Ignored'")
	}
}

func TestDataFromStruct_MixedTags(t *testing.T) {
	s := mixedStruct{Tagged: "hello", Untagged: "world"}

	rs, err := Compile([]byte(`
rule Check {
    when { label != "" }
    then Match {}
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	dc := DataFromStruct(s, rs)

	v := dc.Get("label")
	if v.IsNull() {
		t.Error("labeled field: expected non-null")
	}

	v = dc.Get("Untagged")
	if !v.IsNull() {
		t.Error("untagged field: expected null")
	}
}

func TestDataFromStruct_SliceField(t *testing.T) {
	s := sliceStruct{Tags: []string{"go", "rules"}}

	rs, err := Compile([]byte(`
rule Check {
    when { tags contains "go" }
    then Match {}
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	dc := DataFromStruct(s, rs)

	v := dc.Get("tags")
	if v.IsNull() {
		t.Error("tags: expected non-null list value")
	}
}

func TestDataFromStruct_RoundTrip(t *testing.T) {
	type Order struct {
		Amount float64 `arb:"order.amount"`
		Region string  `arb:"order.region"`
	}

	src := []byte(`
rule HighValue priority 1 {
    when {
        order.amount > 100
        and order.region == "US"
    }
    then ApplyDiscount {
        type: "percentage",
        amount: 10,
    }
}
`)
	rs, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	order := Order{Amount: 200, Region: "US"}
	dc := DataFromStruct(order, rs)

	matched, err := Eval(rs, dc)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].Name != "HighValue" {
		t.Errorf("expected HighValue, got %s", matched[0].Name)
	}

	// Non-matching case
	orderLow := Order{Amount: 50, Region: "US"}
	dcLow := DataFromStruct(orderLow, rs)

	matched, err = Eval(rs, dcLow)
	if err != nil {
		t.Fatalf("Eval low: %v", err)
	}
	if len(matched) != 0 {
		t.Errorf("expected 0 matches for low amount, got %d", len(matched))
	}
}

func TestDataFromStruct_CacheHit(t *testing.T) {
	// Call DataFromStruct twice with the same type to exercise the sync.Map cache path.
	type Item struct {
		Value string `arb:"item.value"`
	}

	rs, err := Compile([]byte(`
rule Check {
    when { item.value != "" }
    then Match {}
}
`))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	dc1 := DataFromStruct(Item{Value: "first"}, rs)
	dc2 := DataFromStruct(Item{Value: "second"}, rs)

	v1 := dc1.Get("item.value")
	v2 := dc2.Get("item.value")

	if v1.IsNull() {
		t.Error("first item.value: expected non-null")
	}
	if v2.IsNull() {
		t.Error("second item.value: expected non-null")
	}
}
