package arbiter

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"m31labs.dev/arbiter/vm"
)

// structMapping caches the field→key mapping for one struct type.
type structMapping struct {
	fields []fieldEntry
}

// fieldEntry describes how one struct field maps to a fact key.
type fieldEntry struct {
	index    int      // field index in the struct
	key      string   // full dot-notation key (e.g. "task.type")
	segments []string // split on "." for nested map construction
}

// mappingCache stores *structMapping keyed by reflect.Type.
var mappingCache sync.Map

// DataFromStruct creates a DataContext from a typed Go struct.
//
// Fields are mapped to fact keys via the `arb` struct tag.
// Fields without an `arb` tag are ignored.
// Dot notation in tags (e.g. `arb:"task.type"`) produces nested maps so that
// rule expressions like `task.type == "x"` resolve correctly.
//
// The function accepts both struct values and pointers to structs.
// It panics if v is not a struct or pointer-to-struct.
func DataFromStruct[T any](v T, prog *Program) vm.DataContext {
	rv := reflect.ValueOf(v)
	// Dereference pointer.
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			pool := prog.stringPool()
			dc := vm.DataFromMap(map[string]any{}, pool)
			return &evalContextWrapper{inner: dc, pool: pool}
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		panic(fmt.Sprintf("arbiter.DataFromStruct: expected struct, got %s", rv.Kind()))
	}

	rt := rv.Type()
	m := getOrBuildMapping(rt)

	data := make(map[string]any)
	for _, fe := range m.fields {
		fieldVal := rv.Field(fe.index).Interface()
		setNested(data, fe.segments, fieldVal)
	}

	pool := prog.stringPool()
	dc := vm.DataFromMap(data, pool)
	return &evalContextWrapper{inner: dc, pool: pool}
}

// getOrBuildMapping returns a cached structMapping for rt, building it if needed.
func getOrBuildMapping(rt reflect.Type) *structMapping {
	if cached, ok := mappingCache.Load(rt); ok {
		return cached.(*structMapping)
	}

	m := buildMapping(rt)
	mappingCache.Store(rt, m)
	return m
}

// buildMapping inspects struct fields and collects those with an `arb` tag.
func buildMapping(rt reflect.Type) *structMapping {
	m := &structMapping{}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag := f.Tag.Get("arb")
		if tag == "" || tag == "-" {
			continue
		}
		m.fields = append(m.fields, fieldEntry{
			index:    i,
			key:      tag,
			segments: strings.Split(tag, "."),
		})
	}
	return m
}

// setNested inserts value into data following the path described by segments.
// For a single segment it sets data[segments[0]] = value.
// For multiple segments it creates intermediate map[string]any as needed.
func setNested(data map[string]any, segments []string, value any) {
	if len(segments) == 1 {
		data[segments[0]] = value
		return
	}

	current := data
	for i, seg := range segments {
		if i == len(segments)-1 {
			current[seg] = value
			return
		}
		// Ensure an intermediate map exists.
		next, ok := current[seg]
		if !ok {
			child := make(map[string]any)
			current[seg] = child
			current = child
		} else {
			child, ok := next.(map[string]any)
			if !ok {
				// Overwrite non-map with a map (tag conflict — last writer wins).
				child = make(map[string]any)
				current[seg] = child
			}
			current = child
		}
	}
}
