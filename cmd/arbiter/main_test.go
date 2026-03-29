package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/arbiter"
	arbiterv1 "github.com/odvcencio/arbiter/api/arbiter/v1"
	"github.com/odvcencio/arbiter/internal/buildinfo"
	"github.com/odvcencio/arbiter/internal/grpcutil"
	"github.com/odvcencio/arbiter/internal/statusview"
	"google.golang.org/grpc"
)

func TestFormatCLIErrorPreservesDiagnostics(t *testing.T) {
	err := &arbiter.DiagnosticError{
		File:    "/tmp/rules.arb",
		Line:    4,
		Column:  9,
		Message: "parse error near \"then\"",
	}
	if got := formatCLIError(err); got != `/tmp/rules.arb:4:9: parse error near "then"` {
		t.Fatalf("unexpected diagnostic formatting: %q", got)
	}
}

func TestFormatCLIErrorUnwrapsWrappedDiagnostics(t *testing.T) {
	err := &arbiter.DiagnosticError{
		File:    "/tmp/rules.arb",
		Line:    4,
		Column:  9,
		Message: "parse error near \"then\"",
	}
	wrapped := fmt.Errorf("check current file: %w", err)
	if got := formatCLIError(wrapped); got != `/tmp/rules.arb:4:9: parse error near "then"` {
		t.Fatalf("unexpected wrapped diagnostic formatting: %q", got)
	}
}

func TestFormatCLIErrorPreservesPathPositionStrings(t *testing.T) {
	err := errors.New(`/tmp/rules.arb:3:5: parse error near "}"`)
	if got := formatCLIError(err); got != err.Error() {
		t.Fatalf("expected raw diagnostic string, got %q", got)
	}
}

func TestFormatCLIErrorPrefixesGenericErrors(t *testing.T) {
	err := errors.New("boom")
	if got := formatCLIError(err); got != "error: boom" {
		t.Fatalf("unexpected generic formatting: %q", got)
	}
}

func TestParseRemoteInspectConfigSupportsFailOnIssues(t *testing.T) {
	cfg := parseRemoteInspectConfig([]string{
		"grpcs://arbiter.internal:7443",
		"--json",
		"--fail-on-issues",
		"--token", "secret",
		"--ca-file", "/tmp/ca.pem",
		"--server-name", "arbiter.internal",
	})
	if cfg.target != "grpcs://arbiter.internal:7443" || !cfg.jsonOut || !cfg.failOnIssues || cfg.token != "secret" || cfg.caFile != "/tmp/ca.pem" || cfg.serverName != "arbiter.internal" {
		t.Fatalf("unexpected remote inspect config: %+v", cfg)
	}
}

func TestFailOnBlockingIssuesCountsBlockingOnly(t *testing.T) {
	issues := []*arbiterv1.StatusIssue{
		{Severity: string(statusview.SeverityWarning), Code: string(statusview.CodeUpstreamError)},
		{Severity: string(statusview.SeverityError), Code: string(statusview.CodeAuditUnhealthy), Blocking: true},
		{Severity: string(statusview.SeverityError), Code: string(statusview.CodeBundleStale), Blocking: true},
	}
	if got := blockingIssueCount(issues); got != 2 {
		t.Fatalf("blockingIssueCount() = %d, want 2", got)
	}
	if err := failOnBlockingIssues("control status", false, issues); err != nil {
		t.Fatalf("failOnBlockingIssues disabled: %v", err)
	}
	err := failOnBlockingIssues("control status", true, issues)
	if err == nil || !strings.Contains(err.Error(), "2 blocking issue") {
		t.Fatalf("unexpected failOnBlockingIssues error: %v", err)
	}
}

func TestStatusIssuesCmdPrintsFilteredCatalog(t *testing.T) {
	out := captureStdout(t, func() {
		if err := statusIssuesCmd(remoteInspectConfig{}, "runtime"); err != nil {
			t.Fatalf("statusIssuesCmd: %v", err)
		}
	})
	if !strings.Contains(out, "status issue catalog (runtime)") {
		t.Fatalf("expected runtime catalog title, got %s", out)
	}
	if !strings.Contains(out, "product=arbiter build_version="+buildinfo.Version+" operator_contract_version="+buildinfo.OperatorContractVersion) {
		t.Fatalf("expected operator contract stamp, got %s", out)
	}
	if !strings.Contains(out, "code=first_tick_incomplete") {
		t.Fatalf("expected runtime issue code, got %s", out)
	}
	if strings.Contains(out, "code=audit_unhealthy") {
		t.Fatalf("did not expect control-only issue in runtime catalog, got %s", out)
	}
}

func TestStatusIssuesCmdRejectsUnknownSurface(t *testing.T) {
	err := statusIssuesCmd(remoteInspectConfig{}, "wat")
	if err == nil || !strings.Contains(err.Error(), `unknown status surface "wat"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStatusIssuesCmdFetchesRemoteCatalogFromRuntime(t *testing.T) {
	target := newStatusCatalogTestTarget(t, "runtime")
	out := captureStdout(t, func() {
		if err := statusIssuesCmd(remoteInspectConfig{target: "grpc://" + target}, ""); err != nil {
			t.Fatalf("statusIssuesCmd(remote runtime): %v", err)
		}
	})
	if !strings.Contains(out, "status issue catalog (runtime)") {
		t.Fatalf("expected runtime title, got %s", out)
	}
	if !strings.Contains(out, "product=arbiter build_version="+buildinfo.Version+" operator_contract_version="+buildinfo.OperatorContractVersion) {
		t.Fatalf("expected runtime operator stamp, got %s", out)
	}
	if !strings.Contains(out, "code=first_tick_incomplete") {
		t.Fatalf("expected runtime issue code, got %s", out)
	}
	if strings.Contains(out, "code=upstream_error") || strings.Contains(out, "code=audit_unhealthy") {
		t.Fatalf("did not expect non-runtime codes in runtime remote catalog, got %s", out)
	}
}

func TestStatusIssuesCmdDetectsAgentServiceRemotely(t *testing.T) {
	target := newStatusCatalogTestTarget(t, "agent")
	out := captureStdout(t, func() {
		if err := statusIssuesCmd(remoteInspectConfig{target: "grpc://" + target}, ""); err != nil {
			t.Fatalf("statusIssuesCmd(remote agent): %v", err)
		}
	})
	if !strings.Contains(out, "status issue catalog (agent)") {
		t.Fatalf("expected agent title, got %s", out)
	}
	if !strings.Contains(out, "product=arbiter build_version="+buildinfo.Version+" operator_contract_version="+buildinfo.OperatorContractVersion) {
		t.Fatalf("expected agent operator stamp, got %s", out)
	}
	if !strings.Contains(out, "code=upstream_error") {
		t.Fatalf("expected agent issue code, got %s", out)
	}
	if strings.Contains(out, "code=first_tick_incomplete") || strings.Contains(out, "code=audit_unhealthy") {
		t.Fatalf("did not expect runtime/control codes in agent remote catalog, got %s", out)
	}
}

func TestStatusIssuesCmdDetectsControlServiceRemotely(t *testing.T) {
	target := newStatusCatalogTestTarget(t, "control")
	out := captureStdout(t, func() {
		if err := statusIssuesCmd(remoteInspectConfig{target: "grpc://" + target}, ""); err != nil {
			t.Fatalf("statusIssuesCmd(remote control): %v", err)
		}
	})
	if !strings.Contains(out, "status issue catalog (control)") {
		t.Fatalf("expected control title, got %s", out)
	}
	if !strings.Contains(out, "product=arbiter build_version="+buildinfo.Version+" operator_contract_version="+buildinfo.OperatorContractVersion) {
		t.Fatalf("expected control operator stamp, got %s", out)
	}
	if !strings.Contains(out, "code=audit_unhealthy") {
		t.Fatalf("expected control issue code, got %s", out)
	}
	if strings.Contains(out, "code=first_tick_incomplete") || strings.Contains(out, "code=upstream_error") {
		t.Fatalf("did not expect runtime/agent codes in control remote catalog, got %s", out)
	}
}

func TestRunRejectsRemovedEmitCommand(t *testing.T) {
	err := run([]string{"emit", "bundle.arb"})
	if err == nil {
		t.Fatal("expected removed emit command to fail")
	}
	if !strings.Contains(err.Error(), "Unknown command: emit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeRuntimeTarget(t *testing.T) {
	got, secure, err := grpcutil.NormalizeTarget("grpc://127.0.0.1:7081", false, false)
	if err != nil {
		t.Fatalf("normalizeRuntimeTarget grpc://: %v", err)
	}
	if got != "127.0.0.1:7081" || secure {
		t.Fatalf("normalized target = (%q, %v), want (127.0.0.1:7081, false)", got, secure)
	}

	got, secure, err = grpcutil.NormalizeTarget("https://arbiter.internal:7443", false, false)
	if err != nil {
		t.Fatalf("normalizeRuntimeTarget https://: %v", err)
	}
	if got != "arbiter.internal:7443" || !secure {
		t.Fatalf("normalized secure target = (%q, %v), want (arbiter.internal:7443, true)", got, secure)
	}
}

func TestCheckRejectsCompileErrorsInIncludedFiles(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeCLIFile(t, dir, "main.arb", `include "bad.arb"`)
	badPath := writeCLIFile(t, dir, "bad.arb", `
rule BadRollout {
	rollout 101
	when { true }
	then Approved {}
}
`)

	err := check(mainPath)
	if err == nil {
		t.Fatal("expected check to fail")
	}
	if !strings.Contains(err.Error(), badPath+":2:1:") {
		t.Fatalf("expected included file diagnostic, got %v", err)
	}
}

func TestExploreCmdPrintsBundleSummary(t *testing.T) {
	dir := t.TempDir()
	path := writeCLIFile(t, dir, "bundle.arb", `
const SAFE_TEMP = 28 C

fact SensorReading {
	temperature: number<temperature>
}

outcome HeatWarning {
	zone: string
}

strategy RouteHeat returns HeatWarning {
	when { input.hot == true } then AlertNow {
		zone: "zone-a",
	}

	else Ignore {
		zone: "zone-b",
	}
}

worker notify_ops {
	input HeatWarning
	output HeatWarning
	webhook https://hooks.internal/heat
}

arbiter greenhouse {
	poll 30s
	source worker://notify_ops
	on HeatWarning worker notify_ops
}

expert rule HeatStress {
	when { input.hot == true } for 10m
	then emit HeatWarning {
		zone: "zone-a",
	}
}
`)

	out := captureStdout(t, func() {
		if err := exploreCmd(path); err != nil {
			t.Fatalf("exploreCmd: %v", err)
		}
	})
	if !strings.Contains(out, `"fact_schemas"`) || !strings.Contains(out, `"expert_rules"`) {
		t.Fatalf("expected explore output to include schemas and expert rules, got %s", out)
	}
	if !strings.Contains(out, `"data_declarations"`) {
		t.Fatalf("expected explore output to include typed data declarations, got %s", out)
	}
	if !strings.Contains(out, `"strategies"`) {
		t.Fatalf("expected explore output to include strategies, got %s", out)
	}
	if !strings.Contains(out, `"workers"`) || !strings.Contains(out, `"arbiters"`) {
		t.Fatalf("expected explore output to include workers and arbiters, got %s", out)
	}
	if !strings.Contains(out, `"SAFE_TEMP"`) {
		t.Fatalf("expected explore output to include constants, got %s", out)
	}
}

func TestStrategyCmdEvaluatesStrategy(t *testing.T) {
	dir := t.TempDir()
	path := writeCLIFile(t, dir, "bundle.arb", `
outcome CheckoutPath {
	target: string
	reason: string
}

strategy CheckoutRouting returns CheckoutPath {
	when {
		user.country == "US"
	} then Domestic {
		target: "domestic",
		reason: "local",
	}

	else Global {
		target: "global",
		reason: "fallback",
	}
}
`)

	out := captureStdout(t, func() {
		if err := strategyCmd(path, "CheckoutRouting", `{"user":{"country":"US"}}`); err != nil {
			t.Fatalf("strategyCmd: %v", err)
		}
	})
	if !strings.Contains(out, `"selected": "Domestic"`) {
		t.Fatalf("expected strategy output to include selected candidate, got %s", out)
	}
	if !strings.Contains(out, `"outcome": "CheckoutPath"`) {
		t.Fatalf("expected strategy output to include outcome schema, got %s", out)
	}
}

func TestTestCmdRunsSpecs(t *testing.T) {
	dir := t.TempDir()
	bundlePath := writeCLIFile(t, dir, "bundle.arb", `
rule FreeShipping {
	when { user.cart_total >= 35 }
	then ApplyShipping { cost: 0 }
}
`)
	testPath := writeCLIFile(t, dir, "bundle.test.arb", `
test "shipping applies" {
	given {
		user.cart_total: 50
	}
	expect rule FreeShipping matched
	expect action ApplyShipping { cost: 0 }
}
`)

	_ = bundlePath
	out := captureStdout(t, func() {
		if err := testCmd(testPath, true); err != nil {
			t.Fatalf("testCmd: %v", err)
		}
	})
	if !strings.Contains(out, "1 passed, 0 failed") {
		t.Fatalf("expected passing test summary, got %s", out)
	}
}

func TestPrintRuntimeCapabilitiesUsesCanonicalSections(t *testing.T) {
	out := captureStdout(t, func() {
		printRuntimeCapabilities(&arbiterv1.GetRuntimeCapabilitiesResponse{
			ControlTransport: &arbiterv1.RuntimeControlTransport{
				Enabled:          true,
				Address:          "127.0.0.1:7081",
				AuthEnabled:      true,
				TlsEnabled:       true,
				MutualTlsEnabled: true,
			},
			CapabilityTransport: &arbiterv1.RuntimeCapabilityTransport{
				Configured:  true,
				Target:      "plugin.internal:7443",
				AuthEnabled: true,
				TlsEnabled:  true,
				ServerName:  "plugin.internal",
			},
			Plugins: []*arbiterv1.RuntimePluginInfo{{
				Name:    "ops-plugin",
				Version: "1.2.3",
			}},
			Sources: []*arbiterv1.RuntimeSourceCapability{{
				Scheme:      "kafka",
				Owner:       arbiterv1.CapabilityOwner_CAPABILITY_OWNER_PLUGIN,
				Description: "stream facts",
			}},
			Sinks: []*arbiterv1.RuntimeHandlerCapability{{
				Kind:        "slack",
				Owner:       arbiterv1.CapabilityOwner_CAPABILITY_OWNER_HOST,
				Description: "deliver alerts",
			}},
			Workers: []*arbiterv1.RuntimeHandlerCapability{{
				Kind:        "python",
				Owner:       arbiterv1.CapabilityOwner_CAPABILITY_OWNER_PLUGIN,
				Description: "run typed jobs",
			}},
		})
	})

	for _, fragment := range []string{
		"runtime surface",
		"transport:",
		"  control:",
		"  capability:",
		"capabilities:",
		"  plugins:",
		"  sources:",
		"  sinks:",
		"  workers:",
		"plugin.internal:7443",
		"ops-plugin (1.2.3)",
		"kafka [plugin]",
		"slack [host]",
		"python [plugin]",
	} {
		if !strings.Contains(out, fragment) {
			t.Fatalf("expected runtime capability output to contain %q, got %s", fragment, out)
		}
	}
}

func TestPrintRuntimeStatusUsesCanonicalSections(t *testing.T) {
	out := captureStdout(t, func() {
		printRuntimeStatus(&arbiterv1.GetRuntimeStatusResponse{
			Operator: &arbiterv1.OperatorIdentity{
				Product:                 buildinfo.Product,
				BuildVersion:            buildinfo.Version,
				OperatorContractVersion: buildinfo.OperatorContractVersion,
			},
			Readiness: &arbiterv1.RuntimeReadinessStatus{
				Ready:  false,
				Reason: "first tick incomplete",
			},
			Issues: []*arbiterv1.StatusIssue{{
				Severity: "error",
				Scope:    "readiness",
				Subject:  "runtime",
				Code:     string(statusview.CodeFirstTickIncomplete),
				Message:  "first tick incomplete",
				Blocking: true,
			}},
			Transport: &arbiterv1.RuntimeTransportStatus{
				Control: &arbiterv1.RuntimeControlTransport{
					Enabled:          true,
					Address:          "127.0.0.1:7081",
					AuthEnabled:      true,
					TlsEnabled:       true,
					MutualTlsEnabled: true,
				},
				Capability: &arbiterv1.RuntimeCapabilityTransport{
					Configured:  true,
					Target:      "plugin.internal:7443",
					AuthEnabled: true,
					TlsEnabled:  true,
				},
			},
			Capabilities: &arbiterv1.RuntimeCapabilitiesStatus{
				Plugins: []*arbiterv1.RuntimePluginInfo{{Name: "ops-plugin", Version: "1.2.3"}},
				Sources: []*arbiterv1.RuntimeSourceCapability{{Scheme: "kafka", Owner: arbiterv1.CapabilityOwner_CAPABILITY_OWNER_PLUGIN}},
				Sinks:   []*arbiterv1.RuntimeHandlerCapability{{Kind: "slack", Owner: arbiterv1.CapabilityOwner_CAPABILITY_OWNER_HOST}},
				Workers: []*arbiterv1.RuntimeHandlerCapability{{Kind: "python", Owner: arbiterv1.CapabilityOwner_CAPABILITY_OWNER_PLUGIN}},
			},
			Activity: &arbiterv1.RuntimeActivityStatus{
				Ticks:    7,
				Errors:   2,
				Delivery: &arbiterv1.RuntimeDeliveryStatus{Delivered: 3, Enqueued: 2, Retried: 1},
				SourceStatus: []*arbiterv1.RuntimeSourceStatus{{
					Target:    "kafka://prices",
					Alias:     "prices",
					Available: true,
					FactCount: 4,
				}},
				SinkStatus: []*arbiterv1.RuntimeSinkStatus{{
					Key:       "ops",
					Kind:      "slack",
					Target:    "slack://ops",
					Available: true,
				}},
			},
		})
	})

	for _, fragment := range []string{
		"runtime status",
		"operator:",
		"product=arbiter build_version=" + buildinfo.Version + " operator_contract_version=" + buildinfo.OperatorContractVersion,
		"readiness:",
		"reason=first tick incomplete",
		"issues:",
		"severity=error blocking=true scope=readiness subject=runtime code=first_tick_incomplete message=first tick incomplete",
		"transport:",
		"capabilities:",
		"activity:",
		"delivery: delivered=3 enqueued=2 retried=1",
		"source_status:",
		"kafka://prices alias=prices available=true facts=4 failures=0",
		"sink_status:",
		"ops kind=slack target=slack://ops available=true pending=0 ambiguous=0 failures=0",
	} {
		if !strings.Contains(out, fragment) {
			t.Fatalf("expected runtime status output to contain %q, got %s", fragment, out)
		}
	}
}

func TestPrintAgentStatusUsesCanonicalSections(t *testing.T) {
	out := captureStdout(t, func() {
		printAgentStatus(&arbiterv1.GetAgentStatusResponse{
			Operator: &arbiterv1.OperatorIdentity{
				Product:                 buildinfo.Product,
				BuildVersion:            buildinfo.Version,
				OperatorContractVersion: buildinfo.OperatorContractVersion,
			},
			Readiness: &arbiterv1.AgentReadinessStatus{
				Ready:          true,
				MaxStalenessMs: 30000,
				TargetCount:    2,
				ReadyCount:     1,
			},
			Issues: []*arbiterv1.StatusIssue{{
				Severity: "warning",
				Scope:    "upstream",
				Subject:  "control-plane",
				Code:     string(statusview.CodeUpstreamError),
				Message:  "upstream unavailable",
			}},
			Transport: &arbiterv1.AgentTransportStatus{
				Control: &arbiterv1.AgentControlTransport{
					Enabled: true,
					Address: "127.0.0.1:7081",
				},
				Upstream: &arbiterv1.AgentUpstreamTransport{
					Configured:  true,
					Target:      "arbiter.internal:7443",
					AuthEnabled: true,
					TlsEnabled:  true,
				},
			},
			Sync: &arbiterv1.AgentSyncStatus{
				PrimaryName:             "checkout",
				BundleErrorsTotal:       3,
				OverrideErrorsTotal:     1,
				BundleReconnectsTotal:   4,
				OverrideReconnectsTotal: 2,
				LastUpstreamError:       "upstream unavailable",
				Bundles: []*arbiterv1.AgentBundleSyncStatus{{
					Name:                   "checkout",
					BundleId:               "bundle-1",
					Checksum:               "abc123",
					BundleWatchConnected:   true,
					OverrideConfigured:     true,
					OverrideWatchConnected: true,
					StalenessMs:            5,
					OverrideStalenessMs:    7,
					BundleErrorsTotal:      3,
					OverrideErrorsTotal:    1,
					BundleReconnects:       4,
					OverrideReconnects:     2,
				}},
			},
		})
	})

	for _, fragment := range []string{
		"agent status",
		"operator:",
		"product=arbiter build_version=" + buildinfo.Version + " operator_contract_version=" + buildinfo.OperatorContractVersion,
		"readiness:",
		"targets=1/2 max_staleness_ms=30000",
		"issues:",
		"severity=warning blocking=false scope=upstream subject=control-plane code=upstream_error message=upstream unavailable",
		"transport:",
		"arbiter.internal:7443",
		"sync:",
		"primary_name=checkout",
		"errors: bundle=3 override=1",
		"bundles:",
		"checkout bundle_id=bundle-1 bundle_watch=true override_configured=true override_watch=true",
		"checksum=abc123",
	} {
		if !strings.Contains(out, fragment) {
			t.Fatalf("expected agent status output to contain %q, got %s", fragment, out)
		}
	}
}

func TestPrintControlStatusUsesCanonicalSections(t *testing.T) {
	out := captureStdout(t, func() {
		printControlStatus(&arbiterv1.GetControlStatusResponse{
			Operator: &arbiterv1.OperatorIdentity{
				Product:                 buildinfo.Product,
				BuildVersion:            buildinfo.Version,
				OperatorContractVersion: buildinfo.OperatorContractVersion,
			},
			Readiness: &arbiterv1.ControlReadinessStatus{
				Ready:  false,
				Reason: "audit unhealthy",
			},
			Issues: []*arbiterv1.StatusIssue{{
				Severity: "error",
				Scope:    "audit",
				Subject:  "/tmp/decisions.jsonl",
				Code:     string(statusview.CodeAuditUnhealthy),
				Message:  "audit unhealthy: disk full",
				Blocking: true,
			}},
			Transport: &arbiterv1.ControlTransportStatus{
				Control: &arbiterv1.ControlListenerTransport{
					Enabled:          true,
					Address:          "127.0.0.1:8081",
					AuthEnabled:      true,
					TlsEnabled:       true,
					MutualTlsEnabled: true,
				},
			},
			Bundles: &arbiterv1.ControlBundlesStatus{
				PublishedTotal: 2,
				ActiveTotal:    1,
				Persisted:      true,
				File:           "/tmp/bundles.json",
				Healthy:        false,
				WritesTotal:    5,
				ErrorsTotal:    1,
				LastError:      "disk full",
				Active: []*arbiterv1.ControlBundleStatus{{
					Name:              "checkout",
					BundleId:          "bundle-1",
					Checksum:          "abc123",
					PublishedVersions: 2,
					RuleCount:         1,
					FlagCount:         1,
					StrategyCount:     1,
				}},
			},
			Overrides: &arbiterv1.ControlOverridesStatus{
				BundleTotal: 1,
				Rules:       1,
				Flags:       1,
				Strategies:  1,
				Persisted:   true,
				File:        "/tmp/overrides.json",
				Healthy:     false,
				WritesTotal: 4,
				ErrorsTotal: 2,
				LastError:   "read-only file system",
				Bundles: []*arbiterv1.ControlBundleOverrideStatus{{
					Name:       "checkout",
					BundleId:   "bundle-1",
					Rules:      1,
					Flags:      1,
					Strategies: 1,
				}},
			},
			Sessions: &arbiterv1.ControlSessionsStatus{
				Active:      1,
				TtlMs:       1800000,
				MaxCount:    100,
				MaxPerOwner: 5,
				Bundles: []*arbiterv1.ControlSessionBundleStatus{{
					Name:     "checkout",
					BundleId: "bundle-1",
					Active:   1,
				}},
			},
			Audit: &arbiterv1.ControlAuditStatus{
				Configured:  true,
				Kind:        "jsonl",
				Durable:     true,
				File:        "/tmp/decisions.jsonl",
				Healthy:     false,
				WritesTotal: 7,
				ErrorsTotal: 2,
				LastError:   "disk full",
			},
		})
	})

	for _, fragment := range []string{
		"control status",
		"operator:",
		"product=arbiter build_version=" + buildinfo.Version + " operator_contract_version=" + buildinfo.OperatorContractVersion,
		"readiness:",
		"reason=audit unhealthy",
		"issues:",
		"severity=error blocking=true scope=audit subject=/tmp/decisions.jsonl code=audit_unhealthy message=audit unhealthy: disk full",
		"transport:",
		"127.0.0.1:8081",
		"bundles:",
		"published_total=2 active_total=1 persisted=true healthy=false writes=5 errors=1",
		"/tmp/bundles.json",
		"last_error=disk full",
		"checkout bundle_id=bundle-1 versions=2 checksum=abc123",
		"overrides:",
		"bundle_total=1 rules=1 flags=1 flag_rules=0 strategies=1 persisted=true healthy=false writes=4 errors=2",
		"/tmp/overrides.json",
		"last_error=read-only file system",
		"sessions:",
		"active=1 ttl_ms=1800000 max_count=100 max_per_owner=5",
		"audit:",
		"configured=true kind=jsonl durable=true healthy=false writes=7 errors=2",
		"last_error=disk full",
		"/tmp/decisions.jsonl",
	} {
		if !strings.Contains(out, fragment) {
			t.Fatalf("expected control status output to contain %q, got %s", fragment, out)
		}
	}
}

func writeCLIFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

type runtimeStatusCatalogTestServer struct {
	arbiterv1.UnimplementedRuntimeServiceServer
}

func (*runtimeStatusCatalogTestServer) GetStatusIssueCatalog(context.Context, *arbiterv1.GetStatusIssueCatalogRequest) (*arbiterv1.GetStatusIssueCatalogResponse, error) {
	return statusview.ProtoCatalog(statusview.SurfaceRuntime), nil
}

type agentStatusCatalogTestServer struct {
	arbiterv1.UnimplementedAgentServiceServer
}

func (*agentStatusCatalogTestServer) GetStatusIssueCatalog(context.Context, *arbiterv1.GetStatusIssueCatalogRequest) (*arbiterv1.GetStatusIssueCatalogResponse, error) {
	return statusview.ProtoCatalog(statusview.SurfaceAgent), nil
}

type controlStatusCatalogTestServer struct {
	arbiterv1.UnimplementedControlServiceServer
}

func (*controlStatusCatalogTestServer) GetStatusIssueCatalog(context.Context, *arbiterv1.GetStatusIssueCatalogRequest) (*arbiterv1.GetStatusIssueCatalogResponse, error) {
	return statusview.ProtoCatalog(statusview.SurfaceControl), nil
}

func newStatusCatalogTestTarget(t *testing.T, surface string) string {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	switch surface {
	case "runtime":
		arbiterv1.RegisterRuntimeServiceServer(srv, &runtimeStatusCatalogTestServer{})
	case "agent":
		arbiterv1.RegisterAgentServiceServer(srv, &agentStatusCatalogTestServer{})
	case "control":
		arbiterv1.RegisterControlServiceServer(srv, &controlStatusCatalogTestServer{})
	default:
		t.Fatalf("unknown test surface %q", surface)
	}
	go func() {
		_ = srv.Serve(lis)
	}()

	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})
	return lis.Addr().String()
}
