// Package bundle provides binary serialization and obfuscation for compiled
// Arbiter rulesets. Use this to ship pre-compiled bundles to browsers and edge
// runtimes without exposing .arb source, governance parameters, or rule internals.
package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/odvcencio/arbiter/compiler"
	dec "github.com/odvcencio/arbiter/decimal"
	"github.com/odvcencio/arbiter/intern"
)

// Magic bytes and version for the binary format.
var magic = [4]byte{'A', 'R', 'B', '1'}

// ObfuscateOptions controls what gets stripped or hashed in the serialized bundle.
type ObfuscateOptions struct {
	// HashRuleNames replaces rule names with SHA-256 truncated hashes.
	// The VM doesn't need human-readable rule names for evaluation.
	HashRuleNames bool

	// HashSegmentNames replaces segment names with hashes.
	HashSegmentNames bool

	// StripRolloutDetails zeros out rollout BPS, subject, and namespace fields.
	// The client receives a go/no-go decision from the server, not the rollout config.
	StripRolloutDetails bool

	// StripPrereqs removes prerequisite and exclusion arrays.
	// Only meaningful if governance is evaluated server-side.
	StripPrereqs bool

	// PreserveStrings lists string pool indices that must NOT be obfuscated
	// (e.g., action names and param keys the caller reads from results).
	// Computed automatically if nil.
	PreserveStrings map[uint16]bool
}

// Marshal serializes a CompiledRuleset to a binary blob.
func Marshal(rs *compiler.CompiledRuleset) ([]byte, error) {
	return MarshalObfuscated(rs, ObfuscateOptions{})
}

// MarshalObfuscated serializes a CompiledRuleset with obfuscation applied.
func MarshalObfuscated(rs *compiler.CompiledRuleset, opts ObfuscateOptions) ([]byte, error) {
	if rs == nil {
		return nil, fmt.Errorf("nil ruleset")
	}

	// Build the set of string indices we must preserve (action names, param keys).
	preserve := opts.PreserveStrings
	if preserve == nil {
		preserve = computePreservedStrings(rs)
	}

	var buf bytes.Buffer
	buf.Write(magic[:])

	// Constant pool: strings
	strs := rs.Constants.Strings()
	writeU32(&buf, uint32(len(strs)))
	for i, s := range strs {
		if shouldObfuscateString(uint16(i), rs, opts, preserve) {
			s = hashString(s)
		}
		writeString(&buf, s)
	}

	// Constant pool: numbers
	nums := rs.Constants.Numbers()
	writeU32(&buf, uint32(len(nums)))
	for _, n := range nums {
		writeF64(&buf, n)
	}

	// Constant pool: decimals
	decs := rs.Constants.Decimals()
	writeU32(&buf, uint32(len(decs)))
	for _, d := range decs {
		writeString(&buf, d.Text())
		writeString(&buf, d.Unit())
	}

	// Constant pool: lists
	lists := rs.Constants.Lists()
	writeU32(&buf, uint32(len(lists)))
	for _, pv := range lists {
		buf.WriteByte(pv.Typ)
		writeF64(&buf, pv.Num)
		writeU16(&buf, pv.Str)
		writeBool(&buf, pv.Bool)
		writeU16(&buf, pv.ListIdx)
		writeU16(&buf, pv.ListLen)
		writeU16(&buf, pv.Dec)
	}

	// Instructions
	writeU32(&buf, uint32(len(rs.Instructions)))
	buf.Write(rs.Instructions)

	// Rules
	writeU32(&buf, uint32(len(rs.Rules)))
	for _, r := range rs.Rules {
		rule := r
		if opts.StripRolloutDetails {
			rule.HasRollout = false
			rule.RolloutBps = 0
			rule.RolloutSubjectIdx = 0
			rule.RolloutNamespaceIdx = 0
			rule.HasRolloutSubject = false
			rule.HasRolloutNamespace = false
		}
		writeRuleHeader(&buf, rule)
	}

	// Actions
	writeU32(&buf, uint32(len(rs.Actions)))
	for _, a := range rs.Actions {
		writeU16(&buf, a.NameIdx)
		writeU32(&buf, uint32(len(a.Params)))
		for _, p := range a.Params {
			writeU16(&buf, p.KeyIdx)
			writeU32(&buf, p.ValueOff)
			writeU32(&buf, p.ValueLen)
		}
	}

	// Prereqs
	if opts.StripPrereqs {
		writeU32(&buf, 0)
		writeU32(&buf, 0)
	} else {
		writeU32(&buf, uint32(len(rs.Prereqs)))
		for _, idx := range rs.Prereqs {
			writeU16(&buf, idx)
		}
		writeU32(&buf, uint32(len(rs.Excludes)))
		for _, idx := range rs.Excludes {
			writeU16(&buf, idx)
		}
	}

	// Tables
	writeU32(&buf, uint32(len(rs.Tables)))
	for _, tbl := range rs.Tables {
		writeString(&buf, tbl.Name)
		writeU16(&buf, uint16(len(tbl.Columns)))
		for _, col := range tbl.Columns {
			writeString(&buf, col)
		}
		writeU32(&buf, uint32(len(tbl.Rows)))
		for _, row := range tbl.Rows {
			for _, pv := range row {
				buf.WriteByte(pv.Typ)
				writeF64(&buf, pv.Num)
				writeU16(&buf, pv.Str)
				writeBool(&buf, pv.Bool)
				writeU16(&buf, pv.ListIdx)
				writeU16(&buf, pv.ListLen)
				writeU16(&buf, pv.Dec)
			}
		}
	}

	// LookupMetas
	writeU32(&buf, uint32(len(rs.LookupMetas)))
	for _, lm := range rs.LookupMetas {
		writeU16(&buf, lm.TableIdx)
		writeU32(&buf, lm.WhereOff)
		writeU32(&buf, lm.WhereLen)
		writeString(&buf, lm.SortCol)
		writeBool(&buf, lm.SortDesc)
		writeU32(&buf, lm.ElseOff)
		writeU32(&buf, lm.ElseLen)
		writeU16(&buf, uint16(len(lm.ElseKeys)))
		for _, k := range lm.ElseKeys {
			writeString(&buf, k)
		}
	}

	return buf.Bytes(), nil
}

// Unmarshal deserializes a binary blob back into a CompiledRuleset.
func Unmarshal(data []byte) (*compiler.CompiledRuleset, error) {
	r := bytes.NewReader(data)

	var m [4]byte
	if _, err := io.ReadFull(r, m[:]); err != nil {
		return nil, fmt.Errorf("read magic: %w", err)
	}
	if m != magic {
		return nil, fmt.Errorf("invalid bundle magic: %x", m)
	}

	// Strings
	strCount := readU32(r)
	pool := intern.NewPool()
	for range strCount {
		s := readString(r)
		pool.String(s)
	}

	// Numbers
	numCount := readU32(r)
	for range numCount {
		pool.Number(readF64(r))
	}

	// Decimals
	decCount := readU32(r)
	for range decCount {
		text := readString(r)
		unit := readString(r)
		v, err := dec.Parse(text, unit)
		if err != nil {
			return nil, fmt.Errorf("unmarshal decimal: %w", err)
		}
		pool.Decimal(v)
	}

	// Lists
	listCount := readU32(r)
	listItems := make([]intern.PoolValue, listCount)
	for i := range listCount {
		listItems[i] = intern.PoolValue{
			Typ:     readU8(r),
			Num:     readF64(r),
			Str:     readU16(r),
			Bool:    readBool(r),
			ListIdx: readU16(r),
			ListLen: readU16(r),
			Dec:     readU16(r),
		}
	}
	if len(listItems) > 0 {
		pool.RestoreLists(listItems)
	}

	// Instructions
	instrLen := readU32(r)
	instructions := make([]byte, instrLen)
	io.ReadFull(r, instructions)

	// Rules
	ruleCount := readU32(r)
	rules := make([]compiler.RuleHeader, ruleCount)
	for i := range ruleCount {
		rules[i] = readRuleHeader(r)
	}

	// Actions
	actionCount := readU32(r)
	actions := make([]compiler.ActionEntry, actionCount)
	for i := range actionCount {
		actions[i].NameIdx = readU16(r)
		paramCount := readU32(r)
		actions[i].Params = make([]compiler.ActionParam, paramCount)
		for j := range paramCount {
			actions[i].Params[j] = compiler.ActionParam{
				KeyIdx:   readU16(r),
				ValueOff: readU32(r),
				ValueLen: readU32(r),
			}
		}
	}

	// Prereqs / Excludes
	prereqCount := readU32(r)
	prereqs := make([]uint16, prereqCount)
	for i := range prereqCount {
		prereqs[i] = readU16(r)
	}
	excludeCount := readU32(r)
	excludes := make([]uint16, excludeCount)
	for i := range excludeCount {
		excludes[i] = readU16(r)
	}

	// Tables
	tableCount := readU32(r)
	tables := make([]compiler.CompiledTable, tableCount)
	for i := range tableCount {
		tbl := compiler.CompiledTable{}
		tbl.Name = readString(r)
		colCount := readU16(r)
		tbl.Columns = make([]string, colCount)
		for j := range colCount {
			tbl.Columns[j] = readString(r)
		}
		rowCount := readU32(r)
		tbl.Rows = make([][]intern.PoolValue, rowCount)
		for j := range rowCount {
			tbl.Rows[j] = make([]intern.PoolValue, colCount)
			for k := range colCount {
				tbl.Rows[j][k] = intern.PoolValue{
					Typ:     readU8(r),
					Num:     readF64(r),
					Str:     readU16(r),
					Bool:    readBool(r),
					ListIdx: readU16(r),
					ListLen: readU16(r),
					Dec:     readU16(r),
				}
			}
		}
		tables[i] = tbl
	}

	// LookupMetas
	metaCount := readU32(r)
	lookupMetas := make([]compiler.LookupMeta, metaCount)
	for i := range metaCount {
		lm := compiler.LookupMeta{}
		lm.TableIdx = readU16(r)
		lm.WhereOff = readU32(r)
		lm.WhereLen = readU32(r)
		lm.SortCol = readString(r)
		lm.SortDesc = readBool(r)
		lm.ElseOff = readU32(r)
		lm.ElseLen = readU32(r)
		keyCount := readU16(r)
		lm.ElseKeys = make([]string, keyCount)
		for j := range keyCount {
			lm.ElseKeys[j] = readString(r)
		}
		lookupMetas[i] = lm
	}

	return &compiler.CompiledRuleset{
		Constants:    pool,
		Instructions: instructions,
		Rules:        rules,
		Actions:      actions,
		Prereqs:      prereqs,
		Excludes:     excludes,
		Tables:       tables,
		LookupMetas:  lookupMetas,
	}, nil
}

// computePreservedStrings finds string indices that must stay readable:
// action names and param keys (the caller reads these from results).
func computePreservedStrings(rs *compiler.CompiledRuleset) map[uint16]bool {
	keep := make(map[uint16]bool)
	for _, a := range rs.Actions {
		keep[a.NameIdx] = true
		for _, p := range a.Params {
			keep[p.KeyIdx] = true
		}
	}
	return keep
}

func shouldObfuscateString(idx uint16, rs *compiler.CompiledRuleset, opts ObfuscateOptions, preserve map[uint16]bool) bool {
	if preserve[idx] {
		return false
	}
	if opts.HashRuleNames {
		for _, r := range rs.Rules {
			if r.NameIdx == idx {
				return true
			}
		}
	}
	if opts.HashSegmentNames {
		for _, r := range rs.Rules {
			if r.HasSegment && r.SegmentNameIdx == idx {
				return true
			}
		}
	}
	return false
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// --- Binary encoding helpers ---

func writeU16(w *bytes.Buffer, v uint16) { binary.Write(w, binary.LittleEndian, v) }
func writeU32(w *bytes.Buffer, v uint32) { binary.Write(w, binary.LittleEndian, v) }
func writeF64(w *bytes.Buffer, v float64) { binary.Write(w, binary.LittleEndian, v) }
func writeBool(w *bytes.Buffer, v bool) {
	if v {
		w.WriteByte(1)
	} else {
		w.WriteByte(0)
	}
}
func writeString(w *bytes.Buffer, s string) {
	writeU32(w, uint32(len(s)))
	w.WriteString(s)
}
func writeRuleHeader(w *bytes.Buffer, r compiler.RuleHeader) {
	writeU16(w, r.NameIdx)
	binary.Write(w, binary.LittleEndian, r.Priority)
	writeU32(w, r.ConditionOff)
	writeU32(w, r.ConditionLen)
	writeU16(w, r.ActionIdx)
	writeU16(w, r.FallbackIdx)
	writeBool(w, r.KillSwitch)
	writeBool(w, r.HasRollout)
	writeU16(w, r.RolloutBps)
	writeU16(w, r.RolloutSubjectIdx)
	writeU16(w, r.RolloutNamespaceIdx)
	writeBool(w, r.HasRolloutSubject)
	writeBool(w, r.HasRolloutNamespace)
	writeU16(w, r.PrereqOff)
	writeU16(w, r.PrereqLen)
	writeU16(w, r.ExcludeOff)
	writeU16(w, r.ExcludeLen)
	writeU16(w, r.SegmentNameIdx)
	writeBool(w, r.HasSegment)
}

func readU8(r *bytes.Reader) uint8       { var v uint8; binary.Read(r, binary.LittleEndian, &v); return v }
func readI32(r *bytes.Reader) int32      { var v int32; binary.Read(r, binary.LittleEndian, &v); return v }
func readU16(r *bytes.Reader) uint16     { var v uint16; binary.Read(r, binary.LittleEndian, &v); return v }
func readU32(r *bytes.Reader) uint32     { var v uint32; binary.Read(r, binary.LittleEndian, &v); return v }
func readF64(r *bytes.Reader) float64    { var v float64; binary.Read(r, binary.LittleEndian, &v); return v }
func readBool(r *bytes.Reader) bool      { return readU8(r) != 0 }
func readString(r *bytes.Reader) string {
	n := readU32(r)
	buf := make([]byte, n)
	io.ReadFull(r, buf)
	return string(buf)
}
func readRuleHeader(r *bytes.Reader) compiler.RuleHeader {
	return compiler.RuleHeader{
		NameIdx:             readU16(r),
		Priority:            readI32(r),
		ConditionOff:        readU32(r),
		ConditionLen:        readU32(r),
		ActionIdx:           readU16(r),
		FallbackIdx:         readU16(r),
		KillSwitch:          readBool(r),
		HasRollout:          readBool(r),
		RolloutBps:          readU16(r),
		RolloutSubjectIdx:   readU16(r),
		RolloutNamespaceIdx: readU16(r),
		HasRolloutSubject:   readBool(r),
		HasRolloutNamespace: readBool(r),
		PrereqOff:           readU16(r),
		PrereqLen:           readU16(r),
		ExcludeOff:          readU16(r),
		ExcludeLen:          readU16(r),
		SegmentNameIdx:      readU16(r),
		HasSegment:          readBool(r),
	}
}
