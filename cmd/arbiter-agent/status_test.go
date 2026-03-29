package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/odvcencio/arbiter/dataplane"
)

const statusTestInitialSource = `
rule AllowCheckout {
	when {
		user.country == "US"
	}
	then Allow {}
}
`

// Full-repo race runs can push dataplane readiness close to 10s under CPU contention.
const statusTestTimeout = 20 * time.Second

func TestStatusHandlerExposesHealthReadinessAndStatus(t *testing.T) {
	cp := newStatusTestControlPlane(dataplane.Bundle{
		Name:   "checkout",
		Source: []byte(statusTestInitialSource),
	})
	syncer := dataplane.New(cp)
	handler := newStatusHandler(syncer, readinessPolicy{}, agentTransportStatus{
		Control:  newAgentControlTransport("127.0.0.1:7081", nil, nil),
		Upstream: newAgentUpstreamTransport("arbiter.internal:7443", true, true, "arbiter.internal"),
	})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("/healthz status = %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz before sync = %d", rr.Code)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- syncer.Run(ctx, dataplane.BundleLocator{Name: "checkout"}, dataplane.WatchRequest{Name: "checkout", ActiveOnly: true})
	}()

	select {
	case <-syncer.Ready():
	case <-time.After(statusTestTimeout):
		t.Fatal("timed out waiting for readiness")
	}

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("/readyz after sync = %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("/status code = %d", rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if ready, _ := payload["ready"].(bool); !ready {
		t.Fatalf("expected ready status, got %v", payload["ready"])
	}
	readiness, _ := payload["readiness"].(map[string]any)
	if readiness == nil {
		t.Fatal("expected readiness payload")
	}
	if ready, _ := readiness["ready"].(bool); !ready {
		t.Fatalf("expected readiness payload to be ready, got %+v", readiness)
	}
	transport, _ := payload["transport"].(map[string]any)
	if transport == nil {
		t.Fatal("expected transport payload")
	}
	syncStatus, _ := payload["sync"].(map[string]any)
	if syncStatus == nil {
		t.Fatal("expected sync payload")
	}
	control, _ := transport["control"].(map[string]any)
	if control == nil || control["address"] != "127.0.0.1:7081" {
		t.Fatalf("unexpected control transport: %+v", control)
	}
	upstream, _ := transport["upstream"].(map[string]any)
	if upstream == nil || upstream["target"] != "arbiter.internal:7443" {
		t.Fatalf("unexpected upstream transport: %+v", upstream)
	}
	if syncStatus["primary_name"] != "checkout" {
		t.Fatalf("unexpected sync primary name: %+v", syncStatus)
	}
	bundles, _ := payload["bundles"].([]any)
	if len(bundles) == 0 {
		t.Fatal("expected bundle payload")
	}
	bundle, _ := bundles[0].(map[string]any)
	if bundle == nil {
		t.Fatal("expected bundle payload")
	}
	if _, ok := bundle["bundle_id"]; !ok {
		t.Fatal("expected bundle id")
	}
	if _, ok := bundle["checksum"]; !ok {
		t.Fatal("expected bundle checksum")
	}
	for _, key := range []string{
		"bundle_errors_total",
		"override_errors_total",
		"bundle_reconnects_total",
		"override_reconnects_total",
	} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected top-level status field %q", key)
		}
		if _, ok := syncStatus[key]; !ok {
			t.Fatalf("expected sync status field %q in %+v", key, syncStatus)
		}
	}
	if syncBundles, _ := syncStatus["bundles"].([]any); len(syncBundles) != len(bundles) {
		t.Fatalf("expected sync bundles to mirror top-level bundles, got %d vs %d", len(syncBundles), len(bundles))
	}
	for _, key := range []string{
		"bundle_watch_connected",
		"override_configured",
		"override_watch_connected",
		"bundle_errors_total",
		"override_errors_total",
		"bundle_reconnects",
		"override_reconnects",
	} {
		if _, ok := bundle[key]; !ok {
			t.Fatalf("expected bundle status field %q", key)
		}
	}

	cancel()
	if err := <-runErr; err != context.Canceled && err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestNewAgentStatusPayloadExposesCanonicalSections(t *testing.T) {
	status := dataplane.AgentStatus{
		Ready:                   true,
		PrimaryName:             "checkout",
		TargetCount:             2,
		ReadyCount:              1,
		BundleErrorsTotal:       3,
		OverrideErrorsTotal:     1,
		BundleReconnectsTotal:   4,
		OverrideReconnectsTotal: 2,
		LastUpstreamError:       "upstream unavailable",
		LastUpstreamErrorAt:     time.Unix(1710000000, 0).UTC(),
		Bundles: []dataplane.BundleSyncStatus{{
			Name:                 "checkout",
			BundleID:             "bundle-1",
			BundleWatchConnected: true,
		}},
	}

	payload := newAgentStatusPayload(status, "", readinessPolicy{maxStaleness: 30 * time.Second}, agentTransportStatus{
		Control:  newAgentControlTransport("127.0.0.1:7081", nil, nil),
		Upstream: newAgentUpstreamTransport("arbiter.internal:7443", true, true, "arbiter.internal"),
	})

	if !payload.Readiness.Ready || payload.Readiness.MaxStalenessMs != 30000 {
		t.Fatalf("unexpected readiness: %+v", payload.Readiness)
	}
	if payload.Transport.Upstream.Target != "arbiter.internal:7443" || payload.Transport.Control.Address != "127.0.0.1:7081" {
		t.Fatalf("unexpected transport: %+v", payload.Transport)
	}
	if payload.Sync.PrimaryName != "checkout" || payload.Sync.BundleErrorsTotal != 3 {
		t.Fatalf("unexpected sync section: %+v", payload.Sync)
	}
	if payload.PrimaryName != "checkout" || payload.BundleErrorsTotal != 3 {
		t.Fatalf("unexpected legacy payload fields: %+v", payload)
	}

	status.Bundles[0].Name = "mutated"
	if payload.Sync.Bundles[0].Name != "checkout" || payload.Bundles[0].Name != "checkout" {
		t.Fatalf("payload should snapshot bundle sync state, got sync=%+v legacy=%+v", payload.Sync.Bundles, payload.Bundles)
	}
}

func TestProtoAgentStatus(t *testing.T) {
	payload := newAgentStatusPayload(dataplane.AgentStatus{
		Ready:                   true,
		PrimaryName:             "checkout",
		TargetCount:             2,
		ReadyCount:              1,
		BundleErrorsTotal:       3,
		OverrideErrorsTotal:     1,
		BundleReconnectsTotal:   4,
		OverrideReconnectsTotal: 2,
		LastUpstreamError:       "upstream unavailable",
		LastUpstreamErrorAt:     time.Unix(1710000000, 0).UTC(),
		Bundles: []dataplane.BundleSyncStatus{{
			Name:                   "checkout",
			BundleID:               "bundle-1",
			Checksum:               "abc123",
			BundleWatchConnected:   true,
			OverrideConfigured:     true,
			OverrideWatchConnected: true,
		}},
	}, "", readinessPolicy{maxStaleness: 30 * time.Second}, agentTransportStatus{
		Control:  newAgentControlTransport("127.0.0.1:7081", nil, nil),
		Upstream: newAgentUpstreamTransport("arbiter.internal:7443", true, true, "arbiter.internal"),
	})

	resp := protoAgentStatus(payload)
	if !resp.GetReadiness().GetReady() || resp.GetReadiness().GetMaxStalenessMs() != 30000 {
		t.Fatalf("unexpected readiness: %+v", resp.GetReadiness())
	}
	if resp.GetTransport().GetControl().GetAddress() != "127.0.0.1:7081" {
		t.Fatalf("unexpected control transport: %+v", resp.GetTransport().GetControl())
	}
	if resp.GetTransport().GetUpstream().GetTarget() != "arbiter.internal:7443" {
		t.Fatalf("unexpected upstream transport: %+v", resp.GetTransport().GetUpstream())
	}
	if resp.GetSync().GetPrimaryName() != "checkout" || resp.GetSync().GetBundleErrorsTotal() != 3 {
		t.Fatalf("unexpected sync payload: %+v", resp.GetSync())
	}
	if len(resp.GetSync().GetBundles()) != 1 {
		t.Fatalf("unexpected bundle sync payload: %+v", resp.GetSync().GetBundles())
	}
	bundle := resp.GetSync().GetBundles()[0]
	if bundle.GetBundleId() != "bundle-1" || bundle.GetChecksum() != "abc123" {
		t.Fatalf("unexpected bundle sync payload: %+v", bundle)
	}
}

func TestStatusHandlerReadinessThresholdMarksStaleSyncUnready(t *testing.T) {
	cp := newStatusTestControlPlane(dataplane.Bundle{
		Name:   "checkout",
		Source: []byte(statusTestInitialSource),
	})
	syncer := dataplane.New(cp)
	handler := newStatusHandler(syncer, readinessPolicy{maxStaleness: time.Millisecond}, agentTransportStatus{
		Control:  newAgentControlTransport("127.0.0.1:7081", nil, nil),
		Upstream: newAgentUpstreamTransport("arbiter.internal:7443", true, true, "arbiter.internal"),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- syncer.Run(ctx, dataplane.BundleLocator{Name: "checkout"}, dataplane.WatchRequest{Name: "checkout", ActiveOnly: true})
	}()

	select {
	case <-syncer.Ready():
	case <-time.After(statusTestTimeout):
		t.Fatal("timed out waiting for readiness")
	}

	time.Sleep(10 * time.Millisecond)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz stale = %d", rr.Code)
	}
	if body := rr.Body.String(); body == "" || !strings.Contains(body, "stale") {
		t.Fatalf("expected stale readiness reason, got %q", body)
	}

	cancel()
	if err := <-runErr; err != context.Canceled && err != nil {
		t.Fatalf("Run: %v", err)
	}
}

type statusTestControlPlane struct {
	mu     sync.Mutex
	bundle dataplane.Bundle
	stream *statusTestStream
}

func newStatusTestControlPlane(bundle dataplane.Bundle) *statusTestControlPlane {
	return &statusTestControlPlane{
		bundle: bundle,
		stream: newStatusTestStream(),
	}
}

func (c *statusTestControlPlane) GetBundle(_ context.Context, _ dataplane.BundleLocator) (*dataplane.Bundle, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	bundle := c.bundle
	bundle.Source = append([]byte(nil), bundle.Source...)
	return &bundle, nil
}

func (c *statusTestControlPlane) WatchBundles(ctx context.Context, _ dataplane.WatchRequest) (dataplane.BundleStream, error) {
	go func() {
		<-ctx.Done()
		_ = c.stream.Close()
	}()
	return c.stream, nil
}

type statusTestStream struct {
	done chan struct{}
	once sync.Once
}

func newStatusTestStream() *statusTestStream {
	return &statusTestStream{done: make(chan struct{})}
}

func (s *statusTestStream) Recv() (*dataplane.BundleEvent, error) {
	<-s.done
	return nil, io.EOF
}

func (s *statusTestStream) Close() error {
	s.once.Do(func() { close(s.done) })
	return nil
}

var _ dataplane.ControlPlane = (*statusTestControlPlane)(nil)
var _ dataplane.BundleStream = (*statusTestStream)(nil)
