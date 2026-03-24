package bundle_test

import (
	"testing"

	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/bundle"
	"github.com/odvcencio/arbiter/vm"
)

func TestRoundTripMarshalUnmarshal(t *testing.T) {
	prog, err := arbiter.Compile([]byte(`
rule FreeShipping {
	when { order.total >= 100 }
	then ApplyShipping { cost: 0, method: "free" }
}

rule StandardShipping {
	when { order.total < 100 }
	then ApplyShipping { cost: 5.99, method: "standard" }
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	rs := prog.Ruleset

	blob, err := bundle.Marshal(rs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	t.Logf("bundle size: %d bytes", len(blob))

	restored, err := bundle.Unmarshal(blob)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Evaluate with the restored ruleset.
	sp := vm.NewStringPool(restored.Constants.Strings())
	dc := vm.DataFromMap(map[string]any{
		"order": map[string]any{"total": float64(150)},
	}, sp)
	matched, err := vm.EvalWithPool(restored, dc, sp)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(matched) == 0 {
		t.Fatal("no match")
	}
	if matched[0].Action != "ApplyShipping" {
		t.Errorf("expected ApplyShipping, got %s", matched[0].Action)
	}
	if matched[0].Params["method"] != "free" {
		t.Errorf("expected free, got %v", matched[0].Params["method"])
	}
}

func TestObfuscatedBundle(t *testing.T) {
	prog, err := arbiter.Compile([]byte(`
segment vip { user.tier == "gold" }

rule VIPOffer {
	when segment vip { user.cart > 50 }
	then Offer { discount: 20 }
	rollout 50
}

rule Default {
	when { true }
	then Offer { discount: 0 }
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	rs := prog.Ruleset

	blob, err := bundle.MarshalObfuscated(rs, bundle.ObfuscateOptions{
		HashRuleNames:       true,
		HashSegmentNames:    true,
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

	// Rule names should be hashed (not readable).
	strs := restored.Constants.Strings()
	for _, s := range strs {
		if s == "VIPOffer" || s == "Default" || s == "vip" {
			t.Errorf("string pool contains unobfuscated name %q", s)
		}
	}

	// Action names and param keys should be preserved.
	found := false
	for _, s := range strs {
		if s == "Offer" {
			found = true
		}
	}
	if !found {
		t.Error("action name 'Offer' was obfuscated — should be preserved")
	}

	// Rollout should be stripped.
	for _, r := range restored.Rules {
		if r.HasRollout {
			t.Error("rollout details should have been stripped")
		}
	}

	// Prereqs should be stripped.
	if len(restored.Prereqs) > 0 || len(restored.Excludes) > 0 {
		t.Error("prereqs/excludes should have been stripped")
	}

	// Should still eval correctly.
	sp := vm.NewStringPool(restored.Constants.Strings())
	dc := vm.DataFromMap(map[string]any{
		"user": map[string]any{"tier": "gold", "cart": float64(100)},
	}, sp)
	matched, err := vm.EvalWithPool(restored, dc, sp)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(matched) == 0 {
		t.Fatal("no match on obfuscated bundle")
	}
	if matched[0].Action != "Offer" {
		t.Errorf("expected Offer, got %s", matched[0].Action)
	}
}
