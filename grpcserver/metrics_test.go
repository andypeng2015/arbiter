package grpcserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	arbiterv1 "github.com/odvcencio/arbiter/api/arbiter/v1"
	"github.com/odvcencio/arbiter/audit"
	"github.com/odvcencio/arbiter/overrides"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/types/known/structpb"
)

// newTestRegistry creates an isolated prometheus.Registry and registers all
// Arbiter metrics into it.  Each test gets its own instance so that parallel
// tests and repeated runs do not share global state.
func newTestRegistry(t *testing.T) *prometheus.Registry {
	t.Helper()
	// Re-create each var bound to fresh collectors so the registry has its own
	// set of counters and histograms, independent of package-level globals.
	reg := prometheus.NewRegistry()

	evalTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "arbiter_eval_total",
		Help: "Total number of evaluations",
	}, []string{"bundle_name", "mode", "status"})

	evalDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "arbiter_eval_duration_seconds",
		Help:    "Evaluation latency in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"bundle_name", "mode", "status"})

	ruleMatchesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "arbiter_rule_matches_total",
		Help: "Total rule matches",
	}, []string{"bundle_name"})

	expertRoundsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "arbiter_expert_rounds_total",
		Help: "Total expert inference rounds",
	}, []string{"bundle_name"})

	expertMutationsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "arbiter_expert_mutations_total",
		Help: "Total expert mutations",
	}, []string{"bundle_name"})

	flagResolvesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "arbiter_flag_resolves_total",
		Help: "Total flag resolves",
	}, []string{"bundle_name", "flag"})

	bundlePublishTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "arbiter_bundle_publish_total",
		Help: "Total bundles published",
	})

	activeSessions = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "arbiter_active_sessions",
		Help: "Currently active expert sessions",
	}, []string{"bundle_name"})

	RegisterMetrics(reg)
	return reg
}

// metricFamilies gathers all metric families from a registry.
func metricFamilies(t *testing.T, reg *prometheus.Registry) map[string]*dto.MetricFamily {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	out := make(map[string]*dto.MetricFamily, len(mfs))
	for _, mf := range mfs {
		out[mf.GetName()] = mf
	}
	return out
}

// counterValue sums the values of all samples in a counter metric family.
func counterValue(mf *dto.MetricFamily) float64 {
	var total float64
	for _, m := range mf.GetMetric() {
		total += m.GetCounter().GetValue()
	}
	return total
}

const metricsTestSource = `
segment enterprise {
	user.plan == "enterprise"
}

rule HighValue {
	when segment enterprise {
		user.cart_total > 100
	}
	then Approve {
		tier: "gold",
	}
}

flag checkout_v2 type boolean default false {
	when enterprise then true
}
`

// TestRegisterMetrics verifies that RegisterMetrics populates the registry with
// the expected metric names and that eval instrumentation increments counters.
func TestRegisterMetrics(t *testing.T) {
	reg := newTestRegistry(t)

	// Verify all metric names are present after registration (even before any
	// observations, the descriptors are discoverable via Describe).
	expectedNames := []string{
		"arbiter_eval_total",
		"arbiter_eval_duration_seconds",
		"arbiter_rule_matches_total",
		"arbiter_expert_rounds_total",
		"arbiter_expert_mutations_total",
		"arbiter_flag_resolves_total",
		"arbiter_bundle_publish_total",
		"arbiter_active_sessions",
	}

	// Collect descriptor names by probing the registry via a channel.
	descCh := make(chan *prometheus.Desc, 256)
	for _, c := range []prometheus.Collector{
		evalTotal, evalDuration, ruleMatchesTotal, expertRoundsTotal,
		expertMutationsTotal, flagResolvesTotal, bundlePublishTotal, activeSessions,
	} {
		c.Describe(descCh)
	}
	close(descCh)

	described := make(map[string]bool)
	for d := range descCh {
		// d.String() has the form "Desc{fqName: "name", ...}"
		s := d.String()
		for _, name := range expectedNames {
			if strings.Contains(s, `"`+name+`"`) {
				described[name] = true
			}
		}
	}
	for _, name := range expectedNames {
		if !described[name] {
			t.Errorf("metric %q not described", name)
		}
	}

	// Run an actual EvaluateRules call through the server and verify counters
	// increment in the isolated registry.
	srv := NewServer(nil, overrides.NewStore(), audit.NopSink{})
	ctx := context.Background()

	_, err := srv.PublishBundle(ctx, &arbiterv1.PublishBundleRequest{
		Name:   "test-bundle",
		Source: []byte(metricsTestSource),
	})
	if err != nil {
		t.Fatalf("publish bundle: %v", err)
	}

	ctxStruct, _ := structpb.NewStruct(map[string]any{
		"user": map[string]any{
			"plan":       "enterprise",
			"cart_total": 150.0,
		},
	})
	_, err = srv.EvaluateRules(ctx, &arbiterv1.EvaluateRulesRequest{
		BundleName: "test-bundle",
		Context:    ctxStruct,
	})
	if err != nil {
		t.Fatalf("evaluate rules: %v", err)
	}

	mfs := metricFamilies(t, reg)

	// arbiter_eval_total must have been incremented exactly once.
	mf, ok := mfs["arbiter_eval_total"]
	if !ok {
		t.Fatal("arbiter_eval_total not found after eval")
	}
	if got := counterValue(mf); got != 1 {
		t.Errorf("arbiter_eval_total = %v, want 1", got)
	}

	// arbiter_rule_matches_total must be >= 1 (HighValue matched).
	mf, ok = mfs["arbiter_rule_matches_total"]
	if !ok {
		t.Fatal("arbiter_rule_matches_total not found after eval")
	}
	if got := counterValue(mf); got < 1 {
		t.Errorf("arbiter_rule_matches_total = %v, want >= 1", got)
	}

	// arbiter_bundle_publish_total must be 1.
	mf, ok = mfs["arbiter_bundle_publish_total"]
	if !ok {
		t.Fatal("arbiter_bundle_publish_total not found after publish")
	}
	if got := counterValue(mf); got != 1 {
		t.Errorf("arbiter_bundle_publish_total = %v, want 1", got)
	}

	// arbiter_eval_duration_seconds must have an observation.
	if _, ok := mfs["arbiter_eval_duration_seconds"]; !ok {
		t.Fatal("arbiter_eval_duration_seconds not found after eval")
	}
}

// TestFlagMetrics verifies that ResolveFlag increments flag-specific counters.
func TestFlagMetrics(t *testing.T) {
	reg := newTestRegistry(t)

	srv := NewServer(nil, overrides.NewStore(), audit.NopSink{})
	ctx := context.Background()

	_, err := srv.PublishBundle(ctx, &arbiterv1.PublishBundleRequest{
		Name:   "flag-bundle",
		Source: []byte(metricsTestSource),
	})
	if err != nil {
		t.Fatalf("publish bundle: %v", err)
	}

	ctxStruct, _ := structpb.NewStruct(map[string]any{
		"user": map[string]any{"plan": "enterprise"},
	})
	_, err = srv.ResolveFlag(ctx, &arbiterv1.ResolveFlagRequest{
		BundleName: "flag-bundle",
		FlagKey:    "checkout_v2",
		Context:    ctxStruct,
	})
	if err != nil {
		t.Fatalf("resolve flag: %v", err)
	}

	mfs := metricFamilies(t, reg)

	mf, ok := mfs["arbiter_flag_resolves_total"]
	if !ok {
		t.Fatal("arbiter_flag_resolves_total not found after flag resolve")
	}
	if got := counterValue(mf); got != 1 {
		t.Errorf("arbiter_flag_resolves_total = %v, want 1", got)
	}

	// eval_total should also have a governed entry.
	mf, ok = mfs["arbiter_eval_total"]
	if !ok {
		t.Fatal("arbiter_eval_total not found after flag resolve")
	}
	// Find the governed label variant.
	found := false
	for _, m := range mf.GetMetric() {
		for _, lp := range m.GetLabel() {
			if lp.GetName() == "mode" && lp.GetValue() == "governed" {
				found = true
			}
		}
	}
	if !found {
		t.Error("arbiter_eval_total has no metric with mode=governed after ResolveFlag")
	}
}

// TestNewHTTPServer verifies the HTTP server is constructed without panicking.
func TestNewHTTPServer(t *testing.T) {
	reg := prometheus.NewRegistry()
	srv := NewHTTPServer(":0", reg)
	if srv == nil {
		t.Fatal("NewHTTPServer returned nil")
	}
}

func TestNewHTTPServerWithStatusUsesCustomPayload(t *testing.T) {
	reg := prometheus.NewRegistry()
	srv := NewHTTPServerWithStatus(":0", reg, func() any {
		return map[string]any{"service": "arbiter-control", "ready": true}
	})
	if srv == nil {
		t.Fatal("NewHTTPServerWithStatus returned nil")
	}

	rr := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("/status code = %d", rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if payload["service"] != "arbiter-control" || payload["ready"] != true {
		t.Fatalf("unexpected status payload: %+v", payload)
	}
}
