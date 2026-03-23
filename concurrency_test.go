package arbiter

import (
	"sync"
	"testing"
)

func TestConcurrentEval(t *testing.T) {
	rs, err := Compile([]byte(`
rule HighValue {
	when { order.total > 100 }
	then Flag { level: "high" }
}

rule LowValue {
	when { order.total <= 100 }
	then Flag { level: "low" }
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			total := float64(n * 10)
			dc := DataFromMap(map[string]any{"order": map[string]any{"total": total}}, rs)
			matched, err := Eval(rs, dc)
			if err != nil {
				t.Errorf("goroutine %d: %v", n, err)
				return
			}
			if len(matched) == 0 {
				t.Errorf("goroutine %d: no match for total=%v", n, total)
				return
			}
			want := "low"
			if total > 100 {
				want = "high"
			}
			if matched[0].Params["level"] != want {
				t.Errorf("goroutine %d: expected level=%s, got %v", n, want, matched[0].Params["level"])
			}
		}(i)
	}
	wg.Wait()
}

func TestConcurrentEvalGoverned(t *testing.T) {
	full, err := CompileFull([]byte(`
segment vip {
	user.tier == "gold"
}

rule VIPOffer {
	when segment vip { user.cart_total > 50 }
	then Offer { discount: 20 }
	rollout 50
}

rule StandardOffer {
	when { user.cart_total > 50 }
	then Offer { discount: 5 }
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ctx := map[string]any{
				"user": map[string]any{
					"tier":       "gold",
					"cart_total": float64(100 + n),
					"id":         float64(n),
				},
			}
			dc := DataFromMap(ctx, full.Ruleset)
			matched, _, err := EvalGoverned(full.Ruleset, dc, full.Segments, ctx)
			if err != nil {
				t.Errorf("goroutine %d: %v", n, err)
				return
			}
			if len(matched) == 0 {
				t.Errorf("goroutine %d: no match", n)
			}
		}(i)
	}
	wg.Wait()
}

func TestConcurrentEvalStrategy(t *testing.T) {
	full, err := CompileFull([]byte(`
outcome Route {
	target: string
}

strategy Routing returns Route {
	when { req.region == "us" } then US { target: "us-east" }
	when { req.region == "eu" } then EU { target: "eu-west" }
	else Global { target: "global" }
}
`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	regions := []string{"us", "eu", "jp", "br"}
	expected := map[string]string{"us": "US", "eu": "EU", "jp": "Global", "br": "Global"}

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			region := regions[n%len(regions)]
			ctx := map[string]any{"req": map[string]any{"region": region}}
			result, err := EvalStrategy(full, "Routing", ctx)
			if err != nil {
				t.Errorf("goroutine %d: %v", n, err)
				return
			}
			if result.Selected != expected[region] {
				t.Errorf("goroutine %d: region=%s expected=%s got=%s", n, region, expected[region], result.Selected)
			}
		}(i)
	}
	wg.Wait()
}

