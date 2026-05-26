// intern/pool.go
package intern

import (
	"fmt"

	dec "m31labs.dev/arbiter/decimal"
)

// TypeTag identifies the type of a pooled value.
const (
	TypeNull    uint8 = 0
	TypeBool    uint8 = 1
	TypeNumber  uint8 = 2
	TypeString  uint8 = 3
	TypeList    uint8 = 4
	TypeDecimal uint8 = 5

	maxPoolIndex   = 1<<16 - 1
	maxPoolEntries = maxPoolIndex + 1
)

// PoolValue is the VM's value representation used in lists.
type PoolValue struct {
	Typ     uint8
	Num     float64
	Str     uint16 // constant pool string index
	Bool    bool
	ListIdx uint16
	ListLen uint16
	Dec     uint16
}

// Pool stores deduplicated constants for a compiled ruleset.
type Pool struct {
	strings  []string
	numbers  []float64
	lists    []PoolValue
	decimals []dec.Value
	strIndex map[string]uint16
	numIndex map[float64]uint16
	decIndex map[string]uint16
	err      error
}

// NewPool creates an empty constant pool.
func NewPool() *Pool {
	return &Pool{
		strIndex: make(map[string]uint16),
		numIndex: make(map[float64]uint16),
		decIndex: make(map[string]uint16),
	}
}

// String interns a string and returns its index.
func (p *Pool) String(s string) uint16 {
	if idx, ok := p.strIndex[s]; ok {
		return idx
	}
	if p.err != nil {
		return 0
	}
	if len(p.strings) > maxPoolIndex {
		p.setErr(fmt.Errorf("string pool overflow: maximum unique strings is %d", maxPoolEntries))
		return 0
	}
	idx := uint16(len(p.strings))
	p.strings = append(p.strings, s)
	p.strIndex[s] = idx
	return idx
}

// Number interns a number and returns its index.
func (p *Pool) Number(n float64) uint16 {
	if idx, ok := p.numIndex[n]; ok {
		return idx
	}
	if p.err != nil {
		return 0
	}
	if len(p.numbers) > maxPoolIndex {
		p.setErr(fmt.Errorf("number pool overflow: maximum unique numbers is %d", maxPoolEntries))
		return 0
	}
	idx := uint16(len(p.numbers))
	p.numbers = append(p.numbers, n)
	p.numIndex[n] = idx
	return idx
}

// Decimal interns an exact decimal and returns its index.
func (p *Pool) Decimal(v dec.Value) uint16 {
	key := v.String()
	if idx, ok := p.decIndex[key]; ok {
		return idx
	}
	if p.err != nil {
		return 0
	}
	if len(p.decimals) > maxPoolIndex {
		p.setErr(fmt.Errorf("decimal pool overflow: maximum unique decimals is %d", maxPoolEntries))
		return 0
	}
	idx := uint16(len(p.decimals))
	p.decimals = append(p.decimals, v)
	p.decIndex[key] = idx
	return idx
}

// List stores a list of values contiguously and returns (start index, length).
func (p *Pool) List(items []PoolValue) (uint16, uint16) {
	if p.err != nil {
		return 0, 0
	}
	if len(items) > maxPoolIndex {
		p.setErr(fmt.Errorf("list literal overflow: maximum list length is %d", maxPoolIndex))
		return 0, 0
	}
	if len(p.lists) > maxPoolIndex {
		p.setErr(fmt.Errorf("list pool overflow: maximum list start index is %d", maxPoolIndex))
		return 0, 0
	}
	start := uint16(len(p.lists))
	p.lists = append(p.lists, items...)
	return start, uint16(len(items))
}

// GetString returns the string at the given pool index.
func (p *Pool) GetString(idx uint16) string {
	if int(idx) >= len(p.strings) {
		return ""
	}
	return p.strings[idx]
}

// GetNumber returns the number at the given pool index.
func (p *Pool) GetNumber(idx uint16) float64 {
	if int(idx) >= len(p.numbers) {
		return 0
	}
	return p.numbers[idx]
}

// GetList returns a slice of pool values from the flat list storage.
func (p *Pool) GetList(idx, length uint16) []PoolValue {
	start := int(idx)
	end := start + int(length)
	if start > len(p.lists) || end > len(p.lists) {
		return nil
	}
	return p.lists[start:end]
}

// GetDecimal returns the decimal at the given pool index.
func (p *Pool) GetDecimal(idx uint16) dec.Value {
	if int(idx) >= len(p.decimals) {
		return dec.Value{}
	}
	return p.decimals[idx]
}

// StringCount returns the number of unique interned strings.
func (p *Pool) StringCount() int { return len(p.strings) }

// NumberCount returns the number of unique interned numbers.
func (p *Pool) NumberCount() int { return len(p.numbers) }

// DecimalCount returns the number of unique interned decimals.
func (p *Pool) DecimalCount() int { return len(p.decimals) }

// Strings returns all interned strings (for serialization).
func (p *Pool) Strings() []string { return p.strings }

// Numbers returns all interned numbers (for serialization).
func (p *Pool) Numbers() []float64 { return p.numbers }

// Lists returns the flat list storage (for serialization).
func (p *Pool) Lists() []PoolValue { return p.lists }

// Decimals returns all interned decimals.
func (p *Pool) Decimals() []dec.Value { return p.decimals }

// RestoreLists replaces the internal list storage (for deserialization).
func (p *Pool) RestoreLists(items []PoolValue) { p.lists = items }

// Err reports the first overflow recorded while interning constants.
func (p *Pool) Err() error {
	if p == nil {
		return nil
	}
	return p.err
}

func (p *Pool) setErr(err error) {
	if p == nil || err == nil || p.err != nil {
		return
	}
	p.err = err
}
