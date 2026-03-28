// vm/datacontext_test.go
package vm

import "testing"

func TestMapContext(t *testing.T) {
	pool := NewStringPool([]string{"name", "age", "missing", "alice"})
	dc := DataFromMap(map[string]any{
		"name": "alice",
		"age":  30.0,
	}, pool)

	v := dc.Get("name")
	if v.Typ != TypeString {
		t.Errorf("name type: got %d, want %d", v.Typ, TypeString)
	}

	v = dc.Get("age")
	if v.Typ != TypeNumber || v.Num != 30.0 {
		t.Errorf("age: got %+v, want number 30", v)
	}

	v = dc.Get("missing")
	if !v.IsNull() {
		t.Error("missing key should return null")
	}
}

func TestNestedMapContext(t *testing.T) {
	pool := NewStringPool([]string{"user.name", "user.age", "alice"})
	dc := DataFromMap(map[string]any{
		"user": map[string]any{
			"name": "alice",
			"age":  25.0,
		},
	}, pool)

	v := dc.Get("user.name")
	if v.Typ != TypeString {
		t.Errorf("user.name type: got %d, want %d", v.Typ, TypeString)
	}

	v = dc.Get("user.age")
	if v.Num != 25.0 {
		t.Errorf("user.age: got %f, want 25", v.Num)
	}
}

func TestJSONContext(t *testing.T) {
	pool := NewStringPool([]string{"name", "bob"})
	dc, err := DataFromJSON(`{"name": "bob"}`, pool)
	if err != nil {
		t.Fatal(err)
	}
	v := dc.Get("name")
	if v.Typ != TypeString {
		t.Errorf("name type: got %d, want %d", v.Typ, TypeString)
	}
}

func TestRuntimeStringsDoNotGrowStringPool(t *testing.T) {
	pool := NewStringPool([]string{"name"})
	beforeStrs := len(pool.strs)
	beforeIndex := len(pool.index)

	v := DataFromMap(map[string]any{"name": "alice"}, pool).Get("name")
	if v.Typ != TypeString {
		t.Fatalf("name type: got %d, want %d", v.Typ, TypeString)
	}
	got, ok := v.Any.(string)
	if !ok || got != "alice" {
		t.Fatalf("dynamic string = %#v, want %q", v.Any, "alice")
	}
	if len(pool.strs) != beforeStrs {
		t.Fatalf("string pool grew from %d to %d", beforeStrs, len(pool.strs))
	}
	if len(pool.index) != beforeIndex {
		t.Fatalf("string pool index grew from %d to %d", beforeIndex, len(pool.index))
	}
}

func TestNewStringPoolRecordsOverflowInsteadOfPanicking(t *testing.T) {
	pool := NewStringPool(make([]string, maxStringPoolEntries+1))
	if pool.Err() == nil {
		t.Fatal("expected constructor overflow to be recorded")
	}
	if got := len(pool.strs); got != maxStringPoolEntries {
		t.Fatalf("string pool length = %d, want %d", got, maxStringPoolEntries)
	}
}

func TestStringPoolInternRecordsOverflowInsteadOfPanicking(t *testing.T) {
	pool := NewStringPool(make([]string, maxStringPoolEntries))
	if idx := pool.Intern("overflow"); idx != 0 {
		t.Fatalf("overflow index = %d, want 0", idx)
	}
	if pool.Err() == nil {
		t.Fatal("expected overflow to be recorded")
	}
}
