package main

import (
	"crypto/tls"
	"strings"
	"testing"

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
	})

	sources := status["sources"].([]map[string]any)
	if len(sources) != 1 || sources[0]["scheme"] != "kafka" || sources[0]["owner"] != "plugin" {
		t.Fatalf("unexpected sources: %+v", sources)
	}

	sinks := status["sinks"].([]map[string]any)
	if len(sinks) != 1 || sinks[0]["kind"] != "discord" || sinks[0]["owner"] != "host" {
		t.Fatalf("unexpected sinks: %+v", sinks)
	}

	workers := status["workers"].([]map[string]any)
	if len(workers) != 1 || workers[0]["kind"] != "python" || workers[0]["owner"] != "plugin" {
		t.Fatalf("unexpected workers: %+v", workers)
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
	if status[0]["name"] != "ops-plugin" || status[0]["version"] != "1.2.3" {
		t.Fatalf("unexpected plugin status: %+v", status[0])
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
