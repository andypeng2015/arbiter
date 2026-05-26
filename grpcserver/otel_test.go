package grpcserver

import (
	"context"
	"testing"

	arbiterv1 "m31labs.dev/arbiter/api/arbiter/v1"
	"m31labs.dev/arbiter/audit"
	"m31labs.dev/arbiter/overrides"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/protobuf/types/known/structpb"
)

func setupTestTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return sr
}

func TestOTelEvalRulesSpan(t *testing.T) {
	sr := setupTestTracer(t)

	reg := NewRegistry()
	srv := NewServer(reg, overrides.NewStore(), audit.NopSink{})

	pub, err := srv.PublishBundle(context.Background(), &arbiterv1.PublishBundleRequest{
		Name:   "otel-test",
		Source: []byte(testSource),
	})
	if err != nil {
		t.Fatalf("PublishBundle: %v", err)
	}

	ctxMap, _ := structpb.NewStruct(map[string]any{
		"user": map[string]any{
			"plan":       "enterprise",
			"cart_total": 150.0,
		},
	})
	_, err = srv.EvaluateRules(context.Background(), &arbiterv1.EvaluateRulesRequest{
		BundleId: pub.BundleId,
		Context:  ctxMap,
	})
	if err != nil {
		t.Fatalf("EvaluateRules: %v", err)
	}

	spans := sr.Ended()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}

	var found bool
	for _, s := range spans {
		if s.Name() == "arbiter.eval.governed" {
			found = true
			attrs := attrMap(s)
			if attrs["arbiter.bundle_name"] != "otel-test" {
				t.Errorf("expected bundle_name=otel-test, got %v", attrs["arbiter.bundle_name"])
			}
			break
		}
	}
	if !found {
		t.Errorf("span arbiter.eval.governed not found; spans: %v", spanNames(spans))
	}
}

func TestOTelResolveFlagSpan(t *testing.T) {
	sr := setupTestTracer(t)

	reg := NewRegistry()
	srv := NewServer(reg, overrides.NewStore(), audit.NopSink{})

	pub, err := srv.PublishBundle(context.Background(), &arbiterv1.PublishBundleRequest{
		Name:   "otel-flag-test",
		Source: []byte(testSource),
	})
	if err != nil {
		t.Fatalf("PublishBundle: %v", err)
	}

	ctxMap, _ := structpb.NewStruct(map[string]any{
		"user": map[string]any{"plan": "enterprise"},
	})
	_, err = srv.ResolveFlag(context.Background(), &arbiterv1.ResolveFlagRequest{
		BundleId: pub.BundleId,
		FlagKey:  "checkout_v2",
		Context:  ctxMap,
	})
	if err != nil {
		t.Fatalf("ResolveFlag: %v", err)
	}

	spans := sr.Ended()
	var found bool
	for _, s := range spans {
		if s.Name() == "arbiter.flag.resolve" {
			found = true
			attrs := attrMap(s)
			if attrs["arbiter.flag.name"] != "checkout_v2" {
				t.Errorf("expected flag.name=checkout_v2, got %v", attrs["arbiter.flag.name"])
			}
			if attrs["arbiter.flag.variant"] != "true" {
				t.Errorf("expected flag.variant=true, got %v", attrs["arbiter.flag.variant"])
			}
			break
		}
	}
	if !found {
		t.Errorf("span arbiter.flag.resolve not found; spans: %v", spanNames(spans))
	}
}

func TestOTelEvalStrategySpan(t *testing.T) {
	sr := setupTestTracer(t)

	reg := NewRegistry()
	srv := NewServer(reg, overrides.NewStore(), audit.NopSink{})

	pub, err := srv.PublishBundle(context.Background(), &arbiterv1.PublishBundleRequest{
		Name:   "otel-strategy-test",
		Source: []byte(testSource),
	})
	if err != nil {
		t.Fatalf("PublishBundle: %v", err)
	}

	ctxMap, _ := structpb.NewStruct(map[string]any{
		"user": map[string]any{"plan": "enterprise"},
	})
	_, err = srv.EvaluateStrategy(context.Background(), &arbiterv1.EvaluateStrategyRequest{
		BundleId:     pub.BundleId,
		StrategyName: "CheckoutRouting",
		Context:      ctxMap,
	})
	if err != nil {
		t.Fatalf("EvaluateStrategy: %v", err)
	}

	spans := sr.Ended()
	var found bool
	for _, s := range spans {
		if s.Name() == "arbiter.eval.strategy" {
			found = true
			attrs := attrMap(s)
			if attrs["arbiter.strategy.name"] != "CheckoutRouting" {
				t.Errorf("expected strategy.name=CheckoutRouting, got %v", attrs["arbiter.strategy.name"])
			}
			if attrs["arbiter.strategy.selected"] != "Priority" {
				t.Errorf("expected strategy.selected=Priority, got %v", attrs["arbiter.strategy.selected"])
			}
			break
		}
	}
	if !found {
		t.Errorf("span arbiter.eval.strategy not found; spans: %v", spanNames(spans))
	}
}

func attrMap(s sdktrace.ReadOnlySpan) map[string]any {
	m := make(map[string]any, len(s.Attributes()))
	for _, kv := range s.Attributes() {
		m[string(kv.Key)] = kv.Value.AsInterface()
	}
	return m
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	names := make([]string, len(spans))
	for i, s := range spans {
		names[i] = s.Name()
	}
	return names
}
