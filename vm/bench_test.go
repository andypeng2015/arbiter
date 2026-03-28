package vm

import (
	"testing"

	"github.com/odvcencio/arbiter/compiler"
	dec "github.com/odvcencio/arbiter/decimal"
	"github.com/odvcencio/arbiter/intern"
)

func benchmarkVM() *VM {
	pool := intern.NewPool()
	return newVM(&compiler.CompiledRuleset{Constants: pool}, NewStringPool(pool.Strings()))
}

func BenchmarkVMPushPop(b *testing.B) {
	vm := benchmarkVM()
	value := NumVal(42)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 64; j++ {
			vm.push(value)
		}
		for j := 0; j < 64; j++ {
			_ = vm.pop()
		}
	}
}

func BenchmarkCloneLocals(b *testing.B) {
	cases := []struct {
		name string
		src  map[string]any
	}{
		{name: "empty", src: nil},
		{name: "small", src: map[string]any{"user": "u_1", "score": 720.0, "country": "US", "vip": true}},
		{name: "medium", src: map[string]any{
			"user":      "u_1",
			"score":     720.0,
			"country":   "US",
			"vip":       true,
			"cart":      199.0,
			"plan":      "enterprise",
			"region":    "na",
			"segment":   "beta",
			"orders":    12,
			"lifetime":  4812.0,
			"eligible":  true,
			"channel":   "web",
			"device":    "desktop",
			"currency":  "USD",
			"risk":      "low",
			"warehouse": "west",
		}},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = cloneLocals(tc.src)
			}
		})
	}
}

func BenchmarkVMValueToAny(b *testing.B) {
	pool := intern.NewPool()
	alphaIdx := pool.String("alpha")
	betaIdx := pool.String("beta")
	listIdx, listLen := pool.List([]intern.PoolValue{
		{Typ: intern.TypeString, Str: alphaIdx},
		{Typ: intern.TypeString, Str: betaIdx},
		{Typ: intern.TypeNumber, Num: 42},
	})
	sp := NewStringPool(pool.Strings())
	vm := newVM(&compiler.CompiledRuleset{Constants: pool}, sp)
	dynamicList := []any{"alpha", "beta", 42.0}
	decimalValue := dec.MustParse("10.25", "USD")

	cases := []struct {
		name  string
		value Value
	}{
		{name: "pooled_string", value: StrVal(alphaIdx)},
		{name: "runtime_string", value: Value{Typ: TypeString, Any: "alpha"}},
		{name: "pooled_list", value: ListVal(listIdx, listLen)},
		{name: "dynamic_list", value: DynListVal(dynamicList)},
		{name: "decimal", value: DecimalVal(decimalValue)},
		{name: "object", value: ObjectVal(map[string]any{"mode": "safe"})},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = vm.valueToAny(tc.value)
			}
		})
	}
}

func BenchmarkVMEvalCondition(b *testing.B) {
	b.Run("numeric_compare", func(b *testing.B) {
		pool := intern.NewPool()
		ageIdx := pool.String("age")
		limitIdx := pool.Number(18)
		var code []byte
		code = compiler.Emit(code, compiler.OpLoadVar, 0, ageIdx)
		code = compiler.Emit(code, compiler.OpLoadNum, 0, limitIdx)
		code = compiler.Emit(code, compiler.OpGt, 0, 0)
		code = compiler.Emit(code, compiler.OpRuleMatch, 0, 0)
		rs := &compiler.CompiledRuleset{
			Constants:    pool,
			Instructions: code,
		}
		sp := NewStringPool(pool.Strings())
		vm := newVM(rs, sp)
		dc := DataFromMap(map[string]any{"age": 25.0}, sp)

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			vm.sp = 0
			vm.err = nil
			if !vm.evalCondition(code, 0, uint32(len(code)), dc) {
				b.Fatal("expected condition to match")
			}
		}
	})

	b.Run("dynamic_string_compare", func(b *testing.B) {
		pool := intern.NewPool()
		nameIdx := pool.String("name")
		aliceIdx := pool.String("alice")
		var code []byte
		code = compiler.Emit(code, compiler.OpLoadVar, 0, nameIdx)
		code = compiler.Emit(code, compiler.OpLoadStr, 0, aliceIdx)
		code = compiler.Emit(code, compiler.OpEq, 0, 0)
		code = compiler.Emit(code, compiler.OpRuleMatch, 0, 0)
		rs := &compiler.CompiledRuleset{
			Constants:    pool,
			Instructions: code,
		}
		sp := NewStringPool(pool.Strings())
		vm := newVM(rs, sp)
		dc := DataFromMap(map[string]any{"name": "alice"}, sp)

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			vm.sp = 0
			vm.err = nil
			if !vm.evalCondition(code, 0, uint32(len(code)), dc) {
				b.Fatal("expected condition to match")
			}
		}
	})
}
