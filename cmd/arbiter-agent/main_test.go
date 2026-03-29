package main

import (
	"crypto/tls"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestBundleNamesFlagSupportsRepeatedCommaSeparatedValues(t *testing.T) {
	var names bundleNamesFlag
	if err := names.Set("checkout, pricing"); err != nil {
		t.Fatalf("Set first value: %v", err)
	}
	if err := names.Set("pricing"); err != nil {
		t.Fatalf("Set repeated value: %v", err)
	}
	if err := names.Set("fraud"); err != nil {
		t.Fatalf("Set second value: %v", err)
	}

	want := []string{"checkout", "pricing", "fraud"}
	if got := names.Values(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Values() = %v, want %v", got, want)
	}
	if got := names.String(); got != "checkout,pricing,fraud" {
		t.Fatalf("String() = %q", got)
	}
}

func TestSplitBundleNamesDropsEmptyEntries(t *testing.T) {
	got := splitBundleNames(" checkout ,, pricing , fraud ")
	want := []string{"checkout", "pricing", "fraud"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitBundleNames() = %v, want %v", got, want)
	}
}

func TestParseDurationEnv(t *testing.T) {
	key := "ARBITER_AGENT_READY_MAX_STALENESS_TEST"
	t.Setenv(key, "45s")

	got, err := parseDurationEnv(key, "0s")
	if err != nil {
		t.Fatalf("parseDurationEnv set env: %v", err)
	}
	if got != 45*time.Second {
		t.Fatalf("parseDurationEnv set env = %v", got)
	}

	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv: %v", err)
	}
	got, err = parseDurationEnv(key, "15s")
	if err != nil {
		t.Fatalf("parseDurationEnv fallback: %v", err)
	}
	if got != 15*time.Second {
		t.Fatalf("parseDurationEnv fallback = %v", got)
	}
}

func TestDescribeUpstreamTransport(t *testing.T) {
	transport, err := describeUpstreamTransport(upstreamDialConfig{
		target:     "https://arbiter.internal:7443",
		token:      "top-secret",
		serverName: "arbiter.internal",
	})
	if err != nil {
		t.Fatalf("describeUpstreamTransport: %v", err)
	}
	if !transport.Configured || transport.Target != "arbiter.internal:7443" {
		t.Fatalf("unexpected upstream target: %+v", transport)
	}
	if !transport.AuthEnabled || !transport.TLSEnabled || transport.ServerName != "arbiter.internal" {
		t.Fatalf("unexpected upstream transport posture: %+v", transport)
	}
}

func TestAgentControlTransportCapturesSecurityPosture(t *testing.T) {
	transport := newAgentControlTransport("0.0.0.0:7081", []string{"top-secret"}, &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert})
	if !transport.Enabled || !transport.PublicListener {
		t.Fatalf("unexpected control transport listener posture: %+v", transport)
	}
	if !transport.AuthEnabled || !transport.TLSEnabled || !transport.MutualTLSEnabled {
		t.Fatalf("unexpected control transport security posture: %+v", transport)
	}
}
