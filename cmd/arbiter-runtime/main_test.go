package main

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	arbiterv1 "m31labs.dev/arbiter/api/arbiter/v1"
	"m31labs.dev/arbiter/capability"
	"m31labs.dev/arbiter/internal/buildinfo"
	"m31labs.dev/arbiter/internal/statusview"
	"m31labs.dev/arbiter/workflow"
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
		runtimeCapabilityTransport{Configured: true, Target: "plugin.internal:7443", TLSEnabled: true},
	)

	if payload.Readiness.Ready || payload.Readiness.Reason != "first tick incomplete" {
		t.Fatalf("unexpected readiness: %+v", payload.Readiness)
	}
	if payload.Operator.BuildVersion != buildinfo.Version || payload.Operator.OperatorContractVersion != buildinfo.OperatorContractVersion {
		t.Fatalf("unexpected operator info: %+v", payload.Operator)
	}
	if len(payload.Issues) != 1 || payload.Issues[0].Code != statusview.CodeFirstTickIncomplete || !payload.Issues[0].Blocking {
		t.Fatalf("unexpected issues: %+v", payload.Issues)
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

func TestRuntimeStatusMuxExposesIssueCatalog(t *testing.T) {
	rr := httptest.NewRecorder()
	(&runtime{}).statusMux().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/status/issues", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("/status/issues code = %d", rr.Code)
	}
	var catalog statusview.Catalog
	if err := json.Unmarshal(rr.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("decode runtime issue catalog: %v", err)
	}
	if catalog.Surface != statusview.SurfaceRuntime {
		t.Fatalf("catalog surface = %q, want runtime", catalog.Surface)
	}
	if catalog.Operator.BuildVersion != buildinfo.Version || catalog.Operator.OperatorContractVersion != buildinfo.OperatorContractVersion {
		t.Fatalf("unexpected operator info: %+v", catalog.Operator)
	}
	if len(catalog.Definitions) == 0 {
		t.Fatal("expected runtime issue catalog")
	}
	found := false
	for _, item := range catalog.Definitions {
		if item.Code == statusview.CodeFirstTickIncomplete {
			found = true
		}
		if item.Code == statusview.CodeAuditUnhealthy {
			t.Fatalf("unexpected control-only code in runtime catalog: %+v", item)
		}
	}
	if !found {
		t.Fatalf("missing runtime readiness code in catalog: %+v", catalog.Definitions)
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

	if resp.GetOperator().GetBuildVersion() != buildinfo.Version || resp.GetOperator().GetOperatorContractVersion() != buildinfo.OperatorContractVersion {
		t.Fatalf("unexpected operator info: %+v", resp.GetOperator())
	}
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

func TestProtoRuntimeStatus(t *testing.T) {
	payload := newRuntimeStatusPayload(
		true,
		9,
		2,
		time.Unix(1710000000, 0).UTC(),
		workflow.TickResult{
			Sources: map[string]workflow.SourceSnapshot{
				"kafka://prices": {
					Target:              "kafka://prices",
					Alias:               "prices",
					Available:           true,
					FactCount:           4,
					ConsecutiveFailures: 1,
				},
			},
			Sinks: map[string]workflow.SinkSnapshot{
				"ops": {
					Key:                 "ops",
					Alias:               "ops",
					Kind:                "slack",
					Target:              "slack://ops",
					Available:           true,
					Pending:             2,
					Ambiguous:           1,
					ConsecutiveFailures: 3,
				},
			},
			Delivered: 5,
			Enqueued:  3,
			Retried:   1,
		},
		workflow.CapabilitySurface{
			Sources: []workflow.SourceCapability{{Scheme: "kafka", Owner: workflow.CapabilityOwnerPlugin}},
			Sinks:   []workflow.HandlerCapability{{Kind: "slack", Owner: workflow.CapabilityOwnerHost}},
			Workers: []workflow.HandlerCapability{{Kind: "python", Owner: workflow.CapabilityOwnerPlugin}},
		},
		&capability.Manifest{Name: "ops-plugin", Version: "1.2.3"},
		runtimeControlTransport{Enabled: true, Address: "127.0.0.1:7081", AuthEnabled: true},
		runtimeCapabilityTransport{Configured: true, Target: "plugin.internal:7443", TLSEnabled: true},
	)

	resp := protoRuntimeStatus(payload)
	if resp.GetOperator().GetBuildVersion() != buildinfo.Version || resp.GetOperator().GetOperatorContractVersion() != buildinfo.OperatorContractVersion {
		t.Fatalf("unexpected operator info: %+v", resp.GetOperator())
	}
	if !resp.GetReadiness().GetReady() || resp.GetReadiness().GetReason() != "" {
		t.Fatalf("unexpected readiness: %+v", resp.GetReadiness())
	}
	if resp.GetTransport().GetControl().GetAddress() != "127.0.0.1:7081" {
		t.Fatalf("unexpected control transport: %+v", resp.GetTransport().GetControl())
	}
	if resp.GetTransport().GetCapability().GetTarget() != "plugin.internal:7443" {
		t.Fatalf("unexpected capability transport: %+v", resp.GetTransport().GetCapability())
	}
	if len(resp.GetIssues()) != 3 {
		t.Fatalf("unexpected runtime issues: %+v", resp.GetIssues())
	}
	if resp.GetIssues()[0].GetCode() != string(statusview.CodeSourceFailures) || resp.GetIssues()[1].GetCode() != string(statusview.CodeSinkFailures) || resp.GetIssues()[2].GetCode() != string(statusview.CodeSinkAmbiguous) {
		t.Fatalf("unexpected runtime issue codes: %+v", resp.GetIssues())
	}
	if len(resp.GetCapabilities().GetPlugins()) != 1 || resp.GetCapabilities().GetPlugins()[0].GetName() != "ops-plugin" {
		t.Fatalf("unexpected capabilities: %+v", resp.GetCapabilities())
	}
	if resp.GetActivity().GetTicks() != 9 || resp.GetActivity().GetDelivery().GetDelivered() != 5 {
		t.Fatalf("unexpected activity counters: %+v", resp.GetActivity())
	}
	if len(resp.GetActivity().GetSourceStatus()) != 1 || resp.GetActivity().GetSourceStatus()[0].GetTarget() != "kafka://prices" {
		t.Fatalf("unexpected source status: %+v", resp.GetActivity().GetSourceStatus())
	}
	if len(resp.GetActivity().GetSinkStatus()) != 1 || resp.GetActivity().GetSinkStatus()[0].GetKey() != "ops" {
		t.Fatalf("unexpected sink status: %+v", resp.GetActivity().GetSinkStatus())
	}
}

func TestRuntimeIssuesExposeTransportWarnings(t *testing.T) {
	issues := runtimeIssues(false, "first tick incomplete", workflow.TickResult{}, runtimeControlTransport{
		Enabled:        true,
		Address:        "0.0.0.0:7081",
		PublicListener: true,
	}, runtimeCapabilityTransport{
		Configured: true,
		Target:     "plugin.internal:7443",
	})
	if len(issues) != 3 {
		t.Fatalf("expected transport warnings plus readiness issue, got %+v", issues)
	}
	if issues[1].Code != statusview.CodePublicControlInsecure || issues[2].Code != statusview.CodeCapabilityTransportInsecure {
		t.Fatalf("unexpected transport issue codes: %+v", issues)
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
