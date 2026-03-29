package main

import (
	"context"
	"errors"
	"testing"
	"time"

	arbiterv1 "github.com/odvcencio/arbiter/api/arbiter/v1"
	"github.com/odvcencio/arbiter/audit"
	"github.com/odvcencio/arbiter/expert"
	"github.com/odvcencio/arbiter/grpcserver"
	"github.com/odvcencio/arbiter/overrides"
)

const controlStatusTestSource = `
rule AllowCheckout {
	when { user.country == "US" }
	then Allow {}
}

flag checkout_v2 type boolean default false {
	when { user.country == "US" } then true
}

outcome CheckoutPath {
	target: string
}

strategy CheckoutRouting returns CheckoutPath {
	when { user.country == "US" } then Priority {
		target: "priority"
	}

	else Standard {
		target: "standard"
	}
}
`

func TestNewControlStatusPayloadExposesCanonicalSections(t *testing.T) {
	dir := t.TempDir()
	bundleFile := dir + "/bundles.json"
	registry, err := grpcserver.NewFileRegistry(bundleFile)
	if err != nil {
		t.Fatalf("NewFileRegistry: %v", err)
	}
	bundle, err := registry.Publish("checkout", []byte(controlStatusTestSource))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := registry.Publish("checkout", []byte(controlStatusTestSource+"\nrule Another { when { true } then Keep {} }\n")); err != nil {
		t.Fatalf("Publish second version: %v", err)
	}

	overridesFile := dir + "/overrides.json"
	store, err := overrides.NewFileStore(overridesFile)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	enabled := true
	rollout := uint16(25)
	if err := store.SetRule(bundle.ID, "AllowCheckout", overrides.RuleOverride{KillSwitch: &enabled, Rollout: &rollout}); err != nil {
		t.Fatalf("SetRule: %v", err)
	}
	if err := store.SetFlag(bundle.ID, "checkout_v2", overrides.FlagOverride{KillSwitch: &enabled}); err != nil {
		t.Fatalf("SetFlag: %v", err)
	}
	if err := store.SetStrategy(bundle.ID, "CheckoutRouting", "Priority", overrides.StrategyOverride{KillSwitch: &enabled, Rollout: &rollout}); err != nil {
		t.Fatalf("SetStrategy: %v", err)
	}

	sessions := grpcserver.NewSessionStore()
	sessions.SetTTL(45 * time.Minute)
	sessions.SetMaxCount(50)
	sessions.SetMaxPerOwner(4)
	if _, err := sessions.CreateForOwner("token:alice", bundle.ID, nil, &expert.Session{}); err != nil {
		t.Fatalf("CreateForOwner: %v", err)
	}

	payload := newControlStatusPayload(
		registry,
		store,
		sessions,
		controlListenerTransport{Enabled: true, Address: "127.0.0.1:8081", AuthEnabled: true, TLSEnabled: true},
		bundleFile,
		overridesFile,
		newControlAuditTracker(true, "jsonl", true, "/tmp/decisions.jsonl"),
	)

	if !payload.Readiness.Ready || payload.Readiness.Reason != "" {
		t.Fatalf("unexpected readiness: %+v", payload.Readiness)
	}
	if payload.Transport.Control.Address != "127.0.0.1:8081" || !payload.Transport.Control.AuthEnabled {
		t.Fatalf("unexpected transport: %+v", payload.Transport)
	}
	if payload.Bundles.PublishedTotal != 2 || payload.Bundles.ActiveTotal != 1 || !payload.Bundles.Persisted || !payload.Bundles.Healthy || payload.Bundles.File != bundleFile || payload.Bundles.WritesTotal == 0 {
		t.Fatalf("unexpected bundles status: %+v", payload.Bundles)
	}
	if len(payload.Bundles.Active) != 1 || payload.Bundles.Active[0].Name != "checkout" || payload.Bundles.Active[0].PublishedVersions != 2 {
		t.Fatalf("unexpected active bundle status: %+v", payload.Bundles.Active)
	}
	if payload.Overrides.BundleTotal != 1 || payload.Overrides.Rules != 1 || payload.Overrides.Flags != 1 || payload.Overrides.Strategies != 1 || !payload.Overrides.Persisted || !payload.Overrides.Healthy || payload.Overrides.File != overridesFile || payload.Overrides.WritesTotal == 0 {
		t.Fatalf("unexpected overrides status: %+v", payload.Overrides)
	}
	if len(payload.Overrides.Bundles) != 1 || payload.Overrides.Bundles[0].Name != "checkout" {
		t.Fatalf("unexpected override bundles: %+v", payload.Overrides.Bundles)
	}
	if payload.Sessions.Active != 1 || payload.Sessions.TTLMS != int64((45*time.Minute)/time.Millisecond) || payload.Sessions.MaxCount != 50 || payload.Sessions.MaxPerOwner != 4 {
		t.Fatalf("unexpected sessions status: %+v", payload.Sessions)
	}
	if len(payload.Sessions.Bundles) != 1 || payload.Sessions.Bundles[0].Name != "checkout" || payload.Sessions.Bundles[0].Active != 1 {
		t.Fatalf("unexpected session bundles: %+v", payload.Sessions.Bundles)
	}
	if !payload.Audit.Configured || payload.Audit.Kind != "jsonl" || !payload.Audit.Durable || payload.Audit.File != "/tmp/decisions.jsonl" || !payload.Audit.Healthy {
		t.Fatalf("unexpected audit status: %+v", payload.Audit)
	}
}

func TestProtoControlStatus(t *testing.T) {
	payload := controlStatusPayload{
		Readiness: controlReadinessStatus{Ready: true},
		Transport: controlTransportStatus{
			Control: controlListenerTransport{
				Enabled:          true,
				Address:          "127.0.0.1:8081",
				PublicListener:   false,
				AuthEnabled:      true,
				TLSEnabled:       true,
				MutualTLSEnabled: true,
			},
		},
		Bundles: controlBundlesStatus{
			PublishedTotal: 2,
			ActiveTotal:    1,
			Persisted:      true,
			File:           "/tmp/bundles.json",
			Healthy:        false,
			WritesTotal:    5,
			ErrorsTotal:    1,
			LastSuccessAt:  time.Unix(1710000000, 0).UTC(),
			LastError:      "disk full",
			LastErrorAt:    time.Unix(1710000060, 0).UTC(),
			Active: []controlBundleStatus{{
				Name:              "checkout",
				BundleID:          "bundle-1",
				Checksum:          "abc123",
				PublishedAt:       time.Unix(1710000000, 0).UTC(),
				PublishedVersions: 2,
				RuleCount:         1,
				FlagCount:         1,
				ExpertRuleCount:   0,
				StrategyCount:     1,
			}},
		},
		Overrides: controlOverridesStatus{
			BundleTotal:   1,
			Rules:         1,
			Flags:         1,
			FlagRules:     0,
			Strategies:    1,
			Persisted:     true,
			File:          "/tmp/overrides.json",
			Healthy:       false,
			WritesTotal:   4,
			ErrorsTotal:   2,
			LastSuccessAt: time.Unix(1710000120, 0).UTC(),
			LastError:     "read-only file system",
			LastErrorAt:   time.Unix(1710000180, 0).UTC(),
			Bundles: []controlBundleOverrideStatus{{
				Name:       "checkout",
				BundleID:   "bundle-1",
				Rules:      1,
				Flags:      1,
				FlagRules:  0,
				Strategies: 1,
			}},
		},
		Sessions: controlSessionsStatus{
			Active:      1,
			TTLMS:       int64((30 * time.Minute) / time.Millisecond),
			MaxCount:    100,
			MaxPerOwner: 5,
			Bundles: []controlSessionBundleStatus{{
				Name:     "checkout",
				BundleID: "bundle-1",
				Active:   1,
			}},
		},
		Audit: controlAuditStatus{
			Configured:    true,
			Kind:          "jsonl",
			Durable:       true,
			File:          "/tmp/decisions.jsonl",
			Healthy:       false,
			WritesTotal:   7,
			ErrorsTotal:   2,
			LastSuccessAt: time.Unix(1710000000, 0).UTC(),
			LastError:     "disk full",
			LastErrorAt:   time.Unix(1710000060, 0).UTC(),
		},
	}

	resp := protoControlStatus(payload)
	if !resp.GetReadiness().GetReady() {
		t.Fatalf("unexpected readiness: %+v", resp.GetReadiness())
	}
	if resp.GetTransport().GetControl().GetAddress() != "127.0.0.1:8081" || !resp.GetTransport().GetControl().GetTlsEnabled() {
		t.Fatalf("unexpected transport: %+v", resp.GetTransport())
	}
	if resp.GetBundles().GetPublishedTotal() != 2 || len(resp.GetBundles().GetActive()) != 1 {
		t.Fatalf("unexpected bundles payload: %+v", resp.GetBundles())
	}
	if resp.GetBundles().GetHealthy() || resp.GetBundles().GetWritesTotal() != 5 || resp.GetBundles().GetErrorsTotal() != 1 || resp.GetBundles().GetLastError() != "disk full" {
		t.Fatalf("unexpected bundle persistence payload: %+v", resp.GetBundles())
	}
	if resp.GetBundles().GetLastSuccessAt() == nil || resp.GetBundles().GetLastErrorAt() == nil {
		t.Fatalf("expected bundle persistence timestamps: %+v", resp.GetBundles())
	}
	if bundle := resp.GetBundles().GetActive()[0]; bundle.GetName() != "checkout" || bundle.GetPublishedVersions() != 2 || bundle.GetChecksum() != "abc123" {
		t.Fatalf("unexpected active bundle: %+v", bundle)
	}
	if resp.GetOverrides().GetBundleTotal() != 1 || len(resp.GetOverrides().GetBundles()) != 1 {
		t.Fatalf("unexpected overrides payload: %+v", resp.GetOverrides())
	}
	if resp.GetOverrides().GetHealthy() || resp.GetOverrides().GetWritesTotal() != 4 || resp.GetOverrides().GetErrorsTotal() != 2 || resp.GetOverrides().GetLastError() != "read-only file system" {
		t.Fatalf("unexpected override persistence payload: %+v", resp.GetOverrides())
	}
	if resp.GetOverrides().GetLastSuccessAt() == nil || resp.GetOverrides().GetLastErrorAt() == nil {
		t.Fatalf("expected override persistence timestamps: %+v", resp.GetOverrides())
	}
	if item := resp.GetOverrides().GetBundles()[0]; item.GetName() != "checkout" || item.GetStrategies() != 1 {
		t.Fatalf("unexpected override bundle: %+v", item)
	}
	if resp.GetSessions().GetActive() != 1 || len(resp.GetSessions().GetBundles()) != 1 {
		t.Fatalf("unexpected sessions payload: %+v", resp.GetSessions())
	}
	if item := resp.GetSessions().GetBundles()[0]; item.GetName() != "checkout" || item.GetActive() != 1 {
		t.Fatalf("unexpected session bundle: %+v", item)
	}
	if !resp.GetAudit().GetConfigured() || resp.GetAudit().GetKind() != "jsonl" || !resp.GetAudit().GetDurable() || resp.GetAudit().GetFile() != "/tmp/decisions.jsonl" {
		t.Fatalf("unexpected audit payload: %+v", resp.GetAudit())
	}
	if resp.GetAudit().GetHealthy() || resp.GetAudit().GetWritesTotal() != 7 || resp.GetAudit().GetErrorsTotal() != 2 || resp.GetAudit().GetLastError() != "disk full" {
		t.Fatalf("unexpected audit health payload: %+v", resp.GetAudit())
	}
	if resp.GetAudit().GetLastSuccessAt() == nil || resp.GetAudit().GetLastErrorAt() == nil {
		t.Fatalf("expected audit timestamps: %+v", resp.GetAudit())
	}
	if got := resp.GetTransport().GetControl().GetMutualTlsEnabled(); !got {
		t.Fatalf("expected mutual TLS to survive proto conversion, got %+v", resp.GetTransport().GetControl())
	}
	if got := resp.GetBundles().GetActive()[0].GetPublishedAt(); got == nil || got.AsTime().IsZero() {
		t.Fatalf("expected published_at timestamp, got %+v", got)
	}
	if _, ok := interface{}(resp).(*arbiterv1.GetControlStatusResponse); !ok {
		t.Fatal("expected control status response type")
	}
}

type auditSinkFunc func(context.Context, audit.DecisionEvent) error

func (f auditSinkFunc) WriteDecision(ctx context.Context, event audit.DecisionEvent) error {
	return f(ctx, event)
}

func TestTrackedAuditSinkHealthTransitions(t *testing.T) {
	tracker := newControlAuditTracker(true, "jsonl", true, "/tmp/decisions.jsonl")
	shouldFail := true
	sink := newTrackedAuditSink(auditSinkFunc(func(context.Context, audit.DecisionEvent) error {
		if shouldFail {
			return errors.New("disk full")
		}
		return nil
	}), tracker, nil)

	err := sink.WriteDecision(context.Background(), audit.DecisionEvent{Kind: "rules"})
	if err == nil {
		t.Fatal("expected audit write to fail")
	}
	first := tracker.Snapshot()
	if first.Healthy || first.ErrorsTotal != 1 || first.LastError != "disk full" || first.WritesTotal != 0 {
		t.Fatalf("unexpected failed snapshot: %+v", first)
	}

	shouldFail = false
	if err := sink.WriteDecision(context.Background(), audit.DecisionEvent{Kind: "rules"}); err != nil {
		t.Fatalf("expected audit write to recover: %v", err)
	}
	second := tracker.Snapshot()
	if !second.Healthy || second.ErrorsTotal != 1 || second.WritesTotal != 1 || second.LastSuccessAt.IsZero() {
		t.Fatalf("unexpected recovered snapshot: %+v", second)
	}
	if second.LastErrorAt.IsZero() {
		t.Fatalf("expected last error time to remain visible: %+v", second)
	}
}

func TestTrackedAuditSinkDiscardModeStaysInspectable(t *testing.T) {
	tracker := newControlAuditTracker(false, "discard", false, "")
	sink := newTrackedAuditSink(audit.NopSink{}, tracker, nil)

	if err := sink.WriteDecision(context.Background(), audit.DecisionEvent{Kind: "rules"}); err != nil {
		t.Fatalf("WriteDecision: %v", err)
	}

	snapshot := tracker.Snapshot()
	if snapshot.Configured || snapshot.Kind != "discard" || snapshot.Durable || !snapshot.Healthy {
		t.Fatalf("unexpected discard snapshot: %+v", snapshot)
	}
	if snapshot.WritesTotal != 0 || snapshot.ErrorsTotal != 0 || !snapshot.LastSuccessAt.IsZero() || snapshot.LastError != "" || !snapshot.LastErrorAt.IsZero() {
		t.Fatalf("discard mode should not surface write counters or timestamps: %+v", snapshot)
	}
}
