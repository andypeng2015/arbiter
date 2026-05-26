package statusview

import (
	"fmt"
	"strings"

	arbiterv1 "m31labs.dev/arbiter/api/arbiter/v1"
	"m31labs.dev/arbiter/internal/buildinfo"
)

type Severity string
type Scope string
type Code string
type Surface string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

const (
	SurfaceRuntime Surface = "runtime"
	SurfaceAgent   Surface = "agent"
	SurfaceControl Surface = "control"
)

const (
	ScopeReadiness Scope = "readiness"
	ScopeTransport Scope = "transport"
	ScopeSource    Scope = "source"
	ScopeSink      Scope = "sink"
	ScopeUpstream  Scope = "upstream"
	ScopeSync      Scope = "sync"
	ScopeBundles   Scope = "bundles"
	ScopeOverrides Scope = "overrides"
	ScopeAudit     Scope = "audit"
)

const (
	CodeStatusUnavailable            Code = "status_unavailable"
	CodeFirstTickIncomplete          Code = "first_tick_incomplete"
	CodeInitialSyncIncomplete        Code = "initial_sync_incomplete"
	CodeNotReady                     Code = "not_ready"
	CodePublicControlInsecure        Code = "public_control_insecure"
	CodeCapabilityTransportInsecure  Code = "capability_transport_insecure"
	CodeUpstreamTransportInsecure    Code = "upstream_transport_insecure"
	CodeSourceUnavailable            Code = "source_unavailable"
	CodeSourceFailures               Code = "source_failures"
	CodeSinkUnavailable              Code = "sink_unavailable"
	CodeSinkFailures                 Code = "sink_failures"
	CodeSinkAmbiguous                Code = "sink_ambiguous"
	CodeUpstreamError                Code = "upstream_error"
	CodeBundleNeverSynced            Code = "bundle_never_synced"
	CodeBundleStale                  Code = "bundle_stale"
	CodeOverrideStale                Code = "override_stale"
	CodeBundleWatchDisconnected      Code = "bundle_watch_disconnected"
	CodeOverrideWatchDisconnected    Code = "override_watch_disconnected"
	CodeBundleSyncError              Code = "bundle_sync_error"
	CodeOverrideSyncError            Code = "override_sync_error"
	CodeBundlePersistenceUnhealthy   Code = "bundle_persistence_unhealthy"
	CodeOverridePersistenceUnhealthy Code = "override_persistence_unhealthy"
	CodeAuditUnhealthy               Code = "audit_unhealthy"
)

// Definition describes one canonical status-issue code.
type Definition struct {
	Code        Code      `json:"code"`
	Severity    Severity  `json:"severity"`
	Scope       Scope     `json:"scope"`
	Blocking    bool      `json:"blocking"`
	Description string    `json:"description"`
	Surfaces    []Surface `json:"surfaces,omitempty"`
}

var issueDefinitions = []Definition{
	{Code: CodeStatusUnavailable, Severity: SeverityError, Scope: ScopeReadiness, Blocking: true, Description: "status payload is unavailable", Surfaces: []Surface{SurfaceControl}},
	{Code: CodeFirstTickIncomplete, Severity: SeverityError, Scope: ScopeReadiness, Blocking: true, Description: "runtime has not completed its first tick", Surfaces: []Surface{SurfaceRuntime}},
	{Code: CodeInitialSyncIncomplete, Severity: SeverityError, Scope: ScopeReadiness, Blocking: true, Description: "agent has not completed its initial sync", Surfaces: []Surface{SurfaceAgent}},
	{Code: CodeNotReady, Severity: SeverityError, Scope: ScopeReadiness, Blocking: true, Description: "surface is not ready for another blocking reason", Surfaces: []Surface{SurfaceAgent}},
	{Code: CodePublicControlInsecure, Severity: SeverityWarning, Scope: ScopeTransport, Blocking: false, Description: "public control listener has no TLS or auth", Surfaces: []Surface{SurfaceRuntime, SurfaceAgent, SurfaceControl}},
	{Code: CodeCapabilityTransportInsecure, Severity: SeverityWarning, Scope: ScopeTransport, Blocking: false, Description: "capability transport has no TLS or auth", Surfaces: []Surface{SurfaceRuntime}},
	{Code: CodeUpstreamTransportInsecure, Severity: SeverityWarning, Scope: ScopeTransport, Blocking: false, Description: "upstream transport has no TLS or auth", Surfaces: []Surface{SurfaceAgent}},
	{Code: CodeSourceUnavailable, Severity: SeverityError, Scope: ScopeSource, Blocking: false, Description: "runtime source is currently unavailable", Surfaces: []Surface{SurfaceRuntime}},
	{Code: CodeSourceFailures, Severity: SeverityWarning, Scope: ScopeSource, Blocking: false, Description: "runtime source has consecutive failures", Surfaces: []Surface{SurfaceRuntime}},
	{Code: CodeSinkUnavailable, Severity: SeverityError, Scope: ScopeSink, Blocking: false, Description: "runtime sink is currently unavailable", Surfaces: []Surface{SurfaceRuntime}},
	{Code: CodeSinkFailures, Severity: SeverityWarning, Scope: ScopeSink, Blocking: false, Description: "runtime sink has consecutive failures", Surfaces: []Surface{SurfaceRuntime}},
	{Code: CodeSinkAmbiguous, Severity: SeverityWarning, Scope: ScopeSink, Blocking: false, Description: "runtime sink has ambiguous deliveries", Surfaces: []Surface{SurfaceRuntime}},
	{Code: CodeUpstreamError, Severity: SeverityWarning, Scope: ScopeUpstream, Blocking: false, Description: "agent observed an upstream control-plane error", Surfaces: []Surface{SurfaceAgent}},
	{Code: CodeBundleNeverSynced, Severity: SeverityError, Scope: ScopeSync, Blocking: true, Description: "bundle has never synced", Surfaces: []Surface{SurfaceAgent}},
	{Code: CodeBundleStale, Severity: SeverityError, Scope: ScopeSync, Blocking: true, Description: "bundle sync is stale", Surfaces: []Surface{SurfaceAgent}},
	{Code: CodeOverrideStale, Severity: SeverityError, Scope: ScopeSync, Blocking: true, Description: "override sync is stale", Surfaces: []Surface{SurfaceAgent}},
	{Code: CodeBundleWatchDisconnected, Severity: SeverityWarning, Scope: ScopeSync, Blocking: false, Description: "bundle watch is disconnected", Surfaces: []Surface{SurfaceAgent}},
	{Code: CodeOverrideWatchDisconnected, Severity: SeverityWarning, Scope: ScopeSync, Blocking: false, Description: "override watch is disconnected", Surfaces: []Surface{SurfaceAgent}},
	{Code: CodeBundleSyncError, Severity: SeverityWarning, Scope: ScopeSync, Blocking: false, Description: "bundle sync has a recorded error", Surfaces: []Surface{SurfaceAgent}},
	{Code: CodeOverrideSyncError, Severity: SeverityWarning, Scope: ScopeSync, Blocking: false, Description: "override sync has a recorded error", Surfaces: []Surface{SurfaceAgent}},
	{Code: CodeBundlePersistenceUnhealthy, Severity: SeverityError, Scope: ScopeBundles, Blocking: true, Description: "bundle persistence is unhealthy", Surfaces: []Surface{SurfaceControl}},
	{Code: CodeOverridePersistenceUnhealthy, Severity: SeverityError, Scope: ScopeOverrides, Blocking: true, Description: "override persistence is unhealthy", Surfaces: []Surface{SurfaceControl}},
	{Code: CodeAuditUnhealthy, Severity: SeverityError, Scope: ScopeAudit, Blocking: true, Description: "audit recording is unhealthy", Surfaces: []Surface{SurfaceControl}},
}

var definitionsByCode = func() map[Code]Definition {
	out := make(map[Code]Definition, len(issueDefinitions))
	for _, item := range issueDefinitions {
		out[item.Code] = item
	}
	return out
}()

// Issue is one canonical operator-facing problem surfaced by runtime, agent,
// and hosted control status endpoints.
type Issue struct {
	Severity Severity `json:"severity"`
	Scope    Scope    `json:"scope"`
	Subject  string   `json:"subject,omitempty"`
	Code     Code     `json:"code"`
	Message  string   `json:"message"`
	Blocking bool     `json:"blocking,omitempty"`
}

type Catalog struct {
	Operator    buildinfo.OperatorInfo `json:"operator"`
	Surface     Surface                `json:"surface,omitempty"`
	Definitions []Definition           `json:"definitions,omitempty"`
}

// Definitions returns the canonical issue-code vocabulary in stable order.
func Definitions() []Definition {
	out := make([]Definition, 0, len(issueDefinitions))
	for _, item := range issueDefinitions {
		out = append(out, cloneDefinition(item))
	}
	return out
}

// DefinitionFor returns the canonical metadata for one issue code.
func DefinitionFor(code Code) (Definition, bool) {
	item, ok := definitionsByCode[code]
	if !ok {
		return Definition{}, false
	}
	return cloneDefinition(item), true
}

// DefinitionsForSurface returns the canonical issue codes that apply to one surface.
func DefinitionsForSurface(surface Surface) []Definition {
	out := make([]Definition, 0)
	for _, item := range issueDefinitions {
		if definitionHasSurface(item, surface) {
			out = append(out, cloneDefinition(item))
		}
	}
	return out
}

func CatalogForSurface(surface Surface) Catalog {
	return Catalog{
		Surface:     surface,
		Operator:    buildinfo.Current(),
		Definitions: DefinitionsForSurface(surface),
	}
}

func CatalogAll() Catalog {
	return Catalog{
		Operator:    buildinfo.Current(),
		Definitions: Definitions(),
	}
}

// New constructs one issue from the canonical code vocabulary.
func New(code Code, subject, message string) Issue {
	item, ok := DefinitionFor(code)
	if !ok {
		panic(fmt.Sprintf("unknown status issue code %q", code))
	}
	return Issue{
		Severity: item.Severity,
		Scope:    item.Scope,
		Subject:  strings.TrimSpace(subject),
		Code:     item.Code,
		Message:  strings.TrimSpace(message),
		Blocking: item.Blocking,
	}
}

func ProtoIssues(items []Issue) []*arbiterv1.StatusIssue {
	if len(items) == 0 {
		return nil
	}
	out := make([]*arbiterv1.StatusIssue, 0, len(items))
	for _, item := range items {
		out = append(out, &arbiterv1.StatusIssue{
			Severity: string(item.Severity),
			Scope:    string(item.Scope),
			Subject:  item.Subject,
			Code:     string(item.Code),
			Message:  item.Message,
			Blocking: item.Blocking,
		})
	}
	return out
}

func ProtoDefinitions() []*arbiterv1.StatusIssueDefinition {
	items := Definitions()
	return protoDefinitions(items)
}

func ProtoDefinitionsForSurface(surface Surface) []*arbiterv1.StatusIssueDefinition {
	return protoDefinitions(DefinitionsForSurface(surface))
}

func ProtoOperator() *arbiterv1.OperatorIdentity {
	item := buildinfo.Current()
	return &arbiterv1.OperatorIdentity{
		Product:                 item.Product,
		BuildVersion:            item.BuildVersion,
		OperatorContractVersion: item.OperatorContractVersion,
	}
}

func ProtoCatalog(surface Surface) *arbiterv1.GetStatusIssueCatalogResponse {
	return &arbiterv1.GetStatusIssueCatalogResponse{
		Definitions: ProtoDefinitionsForSurface(surface),
		Operator:    ProtoOperator(),
		Surface:     string(surface),
	}
}

func protoDefinitions(items []Definition) []*arbiterv1.StatusIssueDefinition {
	out := make([]*arbiterv1.StatusIssueDefinition, 0, len(items))
	for _, item := range items {
		definition := &arbiterv1.StatusIssueDefinition{
			Code:        string(item.Code),
			Severity:    string(item.Severity),
			Scope:       string(item.Scope),
			Blocking:    item.Blocking,
			Description: item.Description,
		}
		for _, surface := range item.Surfaces {
			definition.Surfaces = append(definition.Surfaces, string(surface))
		}
		out = append(out, definition)
	}
	return out
}

func cloneDefinition(item Definition) Definition {
	item.Surfaces = append([]Surface(nil), item.Surfaces...)
	return item
}

func definitionHasSurface(item Definition, surface Surface) bool {
	for _, candidate := range item.Surfaces {
		if candidate == surface {
			return true
		}
	}
	return false
}
