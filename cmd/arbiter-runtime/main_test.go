package main

import (
	"crypto/tls"
	"strings"
	"testing"
	"time"

	arbiterv1 "github.com/odvcencio/arbiter/api/arbiter/v1"
	"github.com/odvcencio/arbiter/capability"
	"github.com/odvcencio/arbiter/workflow"
)

func TestCapabilityStatus(t *testing.T) {
	status := capabilityStatus(workflow.CapabilitySurface{
		Sources: []workflow.SourceCapability{{
			Scheme:      "kafka",
			Owner:       workflow.CapabilityOwnerPlugin,
			Description: "stream facts",
		}},
		Sinks: []workflow.HandlerCapability{{
			Kind:        "discord",
			Owner:       workflow.CapabilityOwnerHost,
			Description: "discord sink",
		}},
		Workers: []workflow.HandlerCapability{{
			Kind:        "python",
			Owner:       workflow.CapabilityOwnerPlugin,
			Description: "python worker",
		}},
	}, nil)

	if len(status.Sources) != 1 || status.Sources[0].Scheme != "kafka" || status.Sources[0].Owner != "plugin" {
		t.Fatalf("unexpected sources: %+v", status.Sources)
	}

	if len(status.Sinks) != 1 || status.Sinks[0].Kind != "discord" || status.Sinks[0].Owner != "host" {
		t.Fatalf("unexpected sinks: %+v", status.Sinks)
	}

	if len(status.Workers) != 1 || status.Workers[0].Kind != "python" || status.Workers[0].Owner != "plugin" {
		t.Fatalf("unexpected workers: %+v", status.Workers)
	}
}

func TestRuntimeTransportStatus(t *testing.T) {
	control := newRuntimeControlTransport("0.0.0.0:7081", []string{"top-secret"}, &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert}, true)
	if !control.Enabled || !control.PublicListener || !control.AuthEnabled || !control.TLSEnabled || !control.MutualTLSEnabled {
		t.Fatalf("unexpected control transport: %+v", control)
	}

	capabilityTransport := newRuntimeCapabilityTransport("plugin.internal:7443", true, true, "plugin.internal")
	if !capabilityTransport.Configured || !capabilityTransport.AuthEnabled || !capabilityTransport.TLSEnabled {
		t.Fatalf("unexpected capability transport: %+v", capabilityTransport)
	}
	if capabilityTransport.ServerName != "plugin.internal" {
		t.Fatalf("capability server name = %q, want plugin.internal", capabilityTransport.ServerName)
	}
}

func TestCapabilityPluginsStatus(t *testing.T) {
	status := capabilityPluginsStatus(&capability.Manifest{
		Name:    "ops-plugin",
		Version: "1.2.3",
	})
	if len(status) != 1 {
		t.Fatalf("status len = %d, want 1", len(status))
	}
	if status[0].Name != "ops-plugin" || status[0].Version != "1.2.3" {
		t.Fatalf("unexpected plugin status: %+v", status[0])
	}
}

func TestRuntimeStatusPayloadExposesCanonicalSections(t *testing.T) {
	lastTick := time.Unix(1710000000, 0).UTC()
	lastResult := workflow.TickResult{
		Sources: map[string]workflow.SourceSnapshot{
			"prices": {
				Alias:     "prices",
				Target:    "kafka://prices",
				Available: true,
			},
		},
		Sinks: map[string]workflow.SinkSnapshot{
			"ops": {
				Alias:     "ops",
				Kind:      "slack",
				Target:    "slack://ops",
				Available: true,
			},
		},
		Delivered: 3,
		Enqueued:  2,
		Retried:   1,
	}
	payload := newRuntimeStatusPayload(
		false,
		7,
		2,
		lastTick,
		lastResult,
		workflow.CapabilitySurface{
			Sources: []workflow.SourceCapability{{
				Scheme: "kafka",
				Owner:  workflow.CapabilityOwnerPlugin,
			}},
			Sinks: []workflow.HandlerCapability{{
				Kind:  "slack",
				Owner: workflow.CapabilityOwnerHost,
			}},
			Workers: []workflow.HandlerCapability{{
				Kind:  "python",
				Owner: workflow.CapabilityOwnerPlugin,
			}},
		},
		&capability.Manifest{Name: "ops-plugin", Version: "1.2.3"},
		runtimeControlTransport{Enabled: true, Address: "127.0.0.1:7081"},
		runtimeCapabilityTransport{Configured: true, Target: "plugin.internal:7443"},
	)

	if payload.Readiness.Ready || payload.Readiness.Reason != "first tick incomplete" {
		t.Fatalf("unexpected readiness: %+v", payload.Readiness)
	}
	if payload.Transport.Control.Address != "127.0.0.1:7081" || payload.Transport.Capability.Target != "plugin.internal:7443" {
		t.Fatalf("unexpected transport: %+v", payload.Transport)
	}
	if len(payload.Capabilities.Plugins) != 1 || payload.Capabilities.Plugins[0].Name != "ops-plugin" {
		t.Fatalf("unexpected capability plugins: %+v", payload.Capabilities.Plugins)
	}
	if payload.Activity.Delivery.Delivered != 3 || payload.Delivered != 3 {
		t.Fatalf("unexpected delivery counters: activity=%+v legacy delivered=%d", payload.Activity.Delivery, payload.Delivered)
	}
	if payload.Activity.SourceStatus["prices"].Target != "kafka://prices" || payload.Sources["prices"].Target != "kafka://prices" {
		t.Fatalf("unexpected source status: activity=%+v legacy=%+v", payload.Activity.SourceStatus, payload.Sources)
	}
	if payload.Activity.SinkStatus["ops"].Kind != "slack" || payload.Sinks["ops"].Kind != "slack" {
		t.Fatalf("unexpected sink status: activity=%+v legacy=%+v", payload.Activity.SinkStatus, payload.Sinks)
	}

	lastResult.Sources["prices"] = workflow.SourceSnapshot{Alias: "mutated"}
	lastResult.Sinks["ops"] = workflow.SinkSnapshot{Alias: "mutated"}
	if payload.Activity.SourceStatus["prices"].Alias != "prices" || payload.Activity.SinkStatus["ops"].Alias != "ops" {
		t.Fatalf("payload should snapshot source/sink status, got sources=%+v sinks=%+v", payload.Activity.SourceStatus, payload.Activity.SinkStatus)
	}
}

func TestProtoRuntimeCapabilities(t *testing.T) {
	resp := protoRuntimeCapabilities(workflow.CapabilitySurface{
		Sources: []workflow.SourceCapability{{
			Scheme:      "kafka",
			Owner:       workflow.CapabilityOwnerPlugin,
			Description: "stream facts",
		}},
		Sinks: []workflow.HandlerCapability{{
			Kind:        "discord",
			Owner:       workflow.CapabilityOwnerHost,
			Description: "discord sink",
		}},
		Workers: []workflow.HandlerCapability{{
			Kind:        "python",
			Owner:       workflow.CapabilityOwnerPlugin,
			Description: "python worker",
		}},
	}, &capability.Manifest{Name: "ops-plugin", Version: "1.2.3"}, runtimeControlTransport{
		Enabled:          true,
		Address:          "127.0.0.1:7081",
		PublicListener:   false,
		AuthEnabled:      true,
		TLSEnabled:       true,
		MutualTLSEnabled: true,
	}, runtimeCapabilityTransport{
		Configured:  true,
		Target:      "plugin.internal:7443",
		AuthEnabled: true,
		TLSEnabled:  true,
		ServerName:  "plugin.internal",
	})

	if len(resp.GetSources()) != 1 || resp.GetSources()[0].GetScheme() != "kafka" {
		t.Fatalf("unexpected proto sources: %+v", resp.GetSources())
	}
	if got := resp.GetSources()[0].GetOwner(); got != arbiterv1.CapabilityOwner_CAPABILITY_OWNER_PLUGIN {
		t.Fatalf("source owner = %v, want plugin", got)
	}
	if len(resp.GetSinks()) != 1 || resp.GetSinks()[0].GetKind() != "discord" {
		t.Fatalf("unexpected proto sinks: %+v", resp.GetSinks())
	}
	if got := resp.GetSinks()[0].GetOwner(); got != arbiterv1.CapabilityOwner_CAPABILITY_OWNER_HOST {
		t.Fatalf("sink owner = %v, want host", got)
	}
	if len(resp.GetWorkers()) != 1 || resp.GetWorkers()[0].GetKind() != "python" {
		t.Fatalf("unexpected proto workers: %+v", resp.GetWorkers())
	}
	if got := resp.GetWorkers()[0].GetOwner(); got != arbiterv1.CapabilityOwner_CAPABILITY_OWNER_PLUGIN {
		t.Fatalf("worker owner = %v, want plugin", got)
	}
	if len(resp.GetPlugins()) != 1 || resp.GetPlugins()[0].GetName() != "ops-plugin" {
		t.Fatalf("unexpected proto plugins: %+v", resp.GetPlugins())
	}
	if !resp.GetControlTransport().GetEnabled() || !resp.GetControlTransport().GetAuthEnabled() || !resp.GetControlTransport().GetTlsEnabled() || !resp.GetControlTransport().GetMutualTlsEnabled() {
		t.Fatalf("unexpected control transport: %+v", resp.GetControlTransport())
	}
	if resp.GetCapabilityTransport().GetTarget() != "plugin.internal:7443" || !resp.GetCapabilityTransport().GetConfigured() || !resp.GetCapabilityTransport().GetTlsEnabled() {
		t.Fatalf("unexpected capability transport: %+v", resp.GetCapabilityTransport())
	}
}

func TestDialCapabilityRuntimeRejectsConflictingTransportHints(t *testing.T) {
	_, _, _, _, err := dialCapabilityRuntime(capabilityDialConfig{
		target:        "grpcs://plugin.internal:7443",
		forceInsecure: true,
	})
	if err == nil {
		t.Fatal("expected conflicting transport hints to fail")
	}
	if !strings.Contains(err.Error(), "plaintext transport") {
		t.Fatalf("unexpected error: %v", err)
	}
}
