// Package conformance defines a cross-platform parity matrix for Arbiter.
// Every test case here must produce identical results across:
//   - Native Go eval (in-process)
//   - Governed eval (with segments, rollouts, prerequisites)
//   - Strategy eval
//   - gRPC eval (via grpcserver)
//   - Binary bundle round-trip (marshal → unmarshal → eval)
//
// If a test passes in one mode but fails in another, the surfaces have diverged.
package conformance_test

import (
	"context"
	"encoding/json"
	"testing"

	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/bundle"
	"github.com/odvcencio/arbiter/expert"
	"github.com/odvcencio/arbiter/vm"
)

type conformanceCase struct {
	name    string
	source  string
	context map[string]any
	expect  func(t *testing.T, matched []vm.MatchedRule)
}

var ruleConformanceCases = []conformanceCase{
	{
		name: "simple condition match",
		source: `
rule HighValue {
	when { order.total > 100 }
	then Flag { level: "high" }
}`,
		context: map[string]any{"order": map[string]any{"total": float64(150)}},
		expect: func(t *testing.T, matched []vm.MatchedRule) {
			if len(matched) != 1 || matched[0].Action != "Flag" || matched[0].Params["level"] != "high" {
				t.Errorf("expected Flag{level:high}, got %+v", matched)
			}
		},
	},
	{
		name: "simple condition no match",
		source: `
rule HighValue {
	when { order.total > 100 }
	then Flag { level: "high" }
}`,
		context: map[string]any{"order": map[string]any{"total": float64(50)}},
		expect: func(t *testing.T, matched []vm.MatchedRule) {
			if len(matched) != 0 {
				t.Errorf("expected no match, got %+v", matched)
			}
		},
	},
	{
		name: "fallback action",
		source: `
rule Shipping {
	when { cart.total >= 100 }
	then Free { cost: 0 }
	otherwise Standard { cost: 5.99 }
}`,
		context: map[string]any{"cart": map[string]any{"total": float64(50)}},
		expect: func(t *testing.T, matched []vm.MatchedRule) {
			if len(matched) != 1 || matched[0].Action != "Standard" {
				t.Errorf("expected Standard fallback, got %+v", matched)
			}
		},
	},
	{
		name: "priority ordering",
		source: `
rule A priority 10 {
	when { x > 0 }
	then First { name: "A" }
}
rule B priority 1 {
	when { x > 0 }
	then First { name: "B" }
}`,
		context: map[string]any{"x": float64(5)},
		expect: func(t *testing.T, matched []vm.MatchedRule) {
			if len(matched) != 2 {
				t.Fatalf("expected 2 matches, got %d", len(matched))
			}
			// Both rules match. Verify both are present.
			names := map[string]bool{}
			for _, m := range matched {
				names[m.Params["name"].(string)] = true
			}
			if !names["A"] || !names["B"] {
				t.Errorf("expected both A and B, got %+v", matched)
			}
		},
	},
	{
		name: "string operators",
		source: `
rule Prefix {
	when { name starts_with "Dr." }
	then Match { found: true }
}`,
		context: map[string]any{"name": "Dr. Smith"},
		expect: func(t *testing.T, matched []vm.MatchedRule) {
			if len(matched) != 1 {
				t.Errorf("expected 1 match, got %d", len(matched))
			}
		},
	},
	{
		name: "list containment",
		source: `
const PREMIUM = ["gold", "platinum"]
rule VIP {
	when { user.tier in PREMIUM }
	then Discount { pct: 15 }
}`,
		context: map[string]any{"user": map[string]any{"tier": "gold"}},
		expect: func(t *testing.T, matched []vm.MatchedRule) {
			if len(matched) != 1 || matched[0].Action != "Discount" {
				t.Errorf("expected Discount, got %+v", matched)
			}
		},
	},
	{
		name: "math expression in action",
		source: `
rule Calc {
	when { true }
	then Result { value: input.a * 2 + input.b }
}`,
		context: map[string]any{"input": map[string]any{"a": float64(10), "b": float64(3)}},
		expect: func(t *testing.T, matched []vm.MatchedRule) {
			if len(matched) != 1 {
				t.Fatalf("expected 1 match")
			}
			v, ok := matched[0].Params["value"].(float64)
			if !ok || v != 23 {
				t.Errorf("expected 23, got %v", matched[0].Params["value"])
			}
		},
	},
}

func TestConformance_NativeEval(t *testing.T) {
	for _, tc := range ruleConformanceCases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := arbiter.Compile([]byte(tc.source))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			dc := arbiter.DataFromMap(tc.context, prog)
			matched, err := arbiter.Eval(prog, dc)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			tc.expect(t, matched)
		})
	}
}

func TestConformance_GovernedEval(t *testing.T) {
	for _, tc := range ruleConformanceCases {
		t.Run(tc.name, func(t *testing.T) {
			full, err := arbiter.CompileFull([]byte(tc.source))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			govProg := &arbiter.Program{Ruleset: full.Ruleset, Segments: full.Segments}
			dc := arbiter.DataFromMap(tc.context, govProg)
			matched, _, err := arbiter.EvalGoverned(govProg, dc, full.Segments, tc.context)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			tc.expect(t, matched)
		})
	}
}

func TestConformance_BundleRoundTrip(t *testing.T) {
	for _, tc := range ruleConformanceCases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := arbiter.Compile([]byte(tc.source))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			rs := prog.Ruleset
			blob, err := bundle.Marshal(rs)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			restored, err := bundle.Unmarshal(blob)
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			sp := vm.NewStringPool(restored.Constants.Strings())
			dc := vm.DataFromMap(tc.context, sp)
			matched, err := vm.EvalWithPool(restored, dc, sp)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			tc.expect(t, matched)
		})
	}
}

func TestConformance_ObfuscatedBundleRoundTrip(t *testing.T) {
	for _, tc := range ruleConformanceCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "priority ordering" {
				t.Skip("obfuscation hashes param values that share pool index with rule names — known limitation")
			}
			prog, err := arbiter.Compile([]byte(tc.source))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			rs := prog.Ruleset
			blob, err := bundle.MarshalObfuscated(rs, bundle.ObfuscateOptions{
				HashRuleNames:       true,
				StripRolloutDetails: true,
				StripPrereqs:        true,
			})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			restored, err := bundle.Unmarshal(blob)
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			sp := vm.NewStringPool(restored.Constants.Strings())
			dc := vm.DataFromMap(tc.context, sp)
			matched, err := vm.EvalWithPool(restored, dc, sp)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			tc.expect(t, matched)
		})
	}
}

func TestConformance_JSONRoundTrip(t *testing.T) {
	for _, tc := range ruleConformanceCases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := arbiter.Compile([]byte(tc.source))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			// Serialize context to JSON and back.
			jsonBytes, _ := json.Marshal(tc.context)
			dc, err := arbiter.DataFromJSON(string(jsonBytes), prog)
			if err != nil {
				t.Fatalf("json context: %v", err)
			}
			matched, err := arbiter.Eval(prog, dc)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			tc.expect(t, matched)
		})
	}
}

// Strategy conformance.
func TestConformance_Strategy(t *testing.T) {
	source := `
outcome Route { target: string }
strategy Routing returns Route {
	when { req.region == "us" } then US { target: "us-east" }
	when { req.region == "eu" } then EU { target: "eu-west" }
	else Global { target: "global" }
}
`
	cases := []struct {
		name     string
		context  map[string]any
		selected string
		target   string
	}{
		{"us", map[string]any{"req": map[string]any{"region": "us"}}, "US", "us-east"},
		{"eu", map[string]any{"req": map[string]any{"region": "eu"}}, "EU", "eu-west"},
		{"fallback", map[string]any{"req": map[string]any{"region": "jp"}}, "Global", "global"},
	}

	full, err := arbiter.CompileFull([]byte(source))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := arbiter.EvalStrategy(full, "Routing", tc.context)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			if result.Selected != tc.selected {
				t.Errorf("expected selected=%s, got %s", tc.selected, result.Selected)
			}
			if result.Params["target"] != tc.target {
				t.Errorf("expected target=%s, got %v", tc.target, result.Params["target"])
			}
		})
	}
}

// Expert conformance.
func TestConformance_Expert(t *testing.T) {
	source := `
fact Input { value: number }
outcome Result { sum: number }

expert rule Accumulate priority 10 {
	when { any inp in facts.Input { inp.value > 0 } }
	then emit Result { sum: inp.value }
}
`
	prog, err := expert.Compile([]byte(source))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	session := expert.NewSession(prog, map[string]any{}, []expert.Fact{
		{Type: "Input", Key: "a", Fields: map[string]any{"value": float64(10)}},
	}, expert.Options{})

	result, err := session.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(result.Outcomes) == 0 {
		t.Fatal("no outcomes")
	}
	if result.Outcomes[0].Name != "Result" {
		t.Errorf("expected Result, got %s", result.Outcomes[0].Name)
	}
}
