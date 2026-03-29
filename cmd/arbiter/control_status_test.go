package main

import (
	"testing"
	"time"

	arbiterv1 "github.com/odvcencio/arbiter/api/arbiter/v1"
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
	registry := grpcserver.NewRegistry()
	bundle, err := registry.Publish("checkout", []byte(controlStatusTestSource))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := registry.Publish("checkout", []byte(controlStatusTestSource+"\nrule Another { when { true } then Keep {} }\n")); err != nil {
		t.Fatalf("Publish second version: %v", err)
	}

	store := overrides.NewStore()
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
		"/tmp/bundles.json",
		"/tmp/overrides.json",
	)

	if !payload.Readiness.Ready || payload.Readiness.Reason != "" {
		t.Fatalf("unexpected readiness: %+v", payload.Readiness)
	}
	if payload.Transport.Control.Address != "127.0.0.1:8081" || !payload.Transport.Control.AuthEnabled {
		t.Fatalf("unexpected transport: %+v", payload.Transport)
	}
	if payload.Bundles.PublishedTotal != 2 || payload.Bundles.ActiveTotal != 1 || !payload.Bundles.Persisted {
		t.Fatalf("unexpected bundles status: %+v", payload.Bundles)
	}
	if len(payload.Bundles.Active) != 1 || payload.Bundles.Active[0].Name != "checkout" || payload.Bundles.Active[0].PublishedVersions != 2 {
		t.Fatalf("unexpected active bundle status: %+v", payload.Bundles.Active)
	}
	if payload.Overrides.BundleTotal != 1 || payload.Overrides.Rules != 1 || payload.Overrides.Flags != 1 || payload.Overrides.Strategies != 1 || !payload.Overrides.Persisted {
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
			BundleTotal: 1,
			Rules:       1,
			Flags:       1,
			FlagRules:   0,
			Strategies:  1,
			Persisted:   true,
			File:        "/tmp/overrides.json",
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
	if bundle := resp.GetBundles().GetActive()[0]; bundle.GetName() != "checkout" || bundle.GetPublishedVersions() != 2 || bundle.GetChecksum() != "abc123" {
		t.Fatalf("unexpected active bundle: %+v", bundle)
	}
	if resp.GetOverrides().GetBundleTotal() != 1 || len(resp.GetOverrides().GetBundles()) != 1 {
		t.Fatalf("unexpected overrides payload: %+v", resp.GetOverrides())
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
