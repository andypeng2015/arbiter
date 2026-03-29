package statusview

import (
	"fmt"
	"strings"

	arbiterv1 "github.com/odvcencio/arbiter/api/arbiter/v1"
)

type Severity string
type Scope string
type Code string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
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
	Code        Code     `json:"code"`
	Severity    Severity `json:"severity"`
	Scope       Scope    `json:"scope"`
	Blocking    bool     `json:"blocking"`
	Description string   `json:"description"`
}

var issueDefinitions = []Definition{
	{Code: CodeStatusUnavailable, Severity: SeverityError, Scope: ScopeReadiness, Blocking: true, Description: "status payload is unavailable"},
	{Code: CodeFirstTickIncomplete, Severity: SeverityError, Scope: ScopeReadiness, Blocking: true, Description: "runtime has not completed its first tick"},
	{Code: CodeInitialSyncIncomplete, Severity: SeverityError, Scope: ScopeReadiness, Blocking: true, Description: "agent has not completed its initial sync"},
	{Code: CodeNotReady, Severity: SeverityError, Scope: ScopeReadiness, Blocking: true, Description: "surface is not ready for another blocking reason"},
	{Code: CodePublicControlInsecure, Severity: SeverityWarning, Scope: ScopeTransport, Blocking: false, Description: "public control listener has no TLS or auth"},
	{Code: CodeCapabilityTransportInsecure, Severity: SeverityWarning, Scope: ScopeTransport, Blocking: false, Description: "capability transport has no TLS or auth"},
	{Code: CodeUpstreamTransportInsecure, Severity: SeverityWarning, Scope: ScopeTransport, Blocking: false, Description: "upstream transport has no TLS or auth"},
	{Code: CodeSourceUnavailable, Severity: SeverityError, Scope: ScopeSource, Blocking: false, Description: "runtime source is currently unavailable"},
	{Code: CodeSourceFailures, Severity: SeverityWarning, Scope: ScopeSource, Blocking: false, Description: "runtime source has consecutive failures"},
	{Code: CodeSinkUnavailable, Severity: SeverityError, Scope: ScopeSink, Blocking: false, Description: "runtime sink is currently unavailable"},
	{Code: CodeSinkFailures, Severity: SeverityWarning, Scope: ScopeSink, Blocking: false, Description: "runtime sink has consecutive failures"},
	{Code: CodeSinkAmbiguous, Severity: SeverityWarning, Scope: ScopeSink, Blocking: false, Description: "runtime sink has ambiguous deliveries"},
	{Code: CodeUpstreamError, Severity: SeverityWarning, Scope: ScopeUpstream, Blocking: false, Description: "agent observed an upstream control-plane error"},
	{Code: CodeBundleNeverSynced, Severity: SeverityError, Scope: ScopeSync, Blocking: true, Description: "bundle has never synced"},
	{Code: CodeBundleStale, Severity: SeverityError, Scope: ScopeSync, Blocking: true, Description: "bundle sync is stale"},
	{Code: CodeOverrideStale, Severity: SeverityError, Scope: ScopeSync, Blocking: true, Description: "override sync is stale"},
	{Code: CodeBundleWatchDisconnected, Severity: SeverityWarning, Scope: ScopeSync, Blocking: false, Description: "bundle watch is disconnected"},
	{Code: CodeOverrideWatchDisconnected, Severity: SeverityWarning, Scope: ScopeSync, Blocking: false, Description: "override watch is disconnected"},
	{Code: CodeBundleSyncError, Severity: SeverityWarning, Scope: ScopeSync, Blocking: false, Description: "bundle sync has a recorded error"},
	{Code: CodeOverrideSyncError, Severity: SeverityWarning, Scope: ScopeSync, Blocking: false, Description: "override sync has a recorded error"},
	{Code: CodeBundlePersistenceUnhealthy, Severity: SeverityError, Scope: ScopeBundles, Blocking: true, Description: "bundle persistence is unhealthy"},
	{Code: CodeOverridePersistenceUnhealthy, Severity: SeverityError, Scope: ScopeOverrides, Blocking: true, Description: "override persistence is unhealthy"},
	{Code: CodeAuditUnhealthy, Severity: SeverityError, Scope: ScopeAudit, Blocking: true, Description: "audit recording is unhealthy"},
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

// Definitions returns the canonical issue-code vocabulary in stable order.
func Definitions() []Definition {
	return append([]Definition(nil), issueDefinitions...)
}

// DefinitionFor returns the canonical metadata for one issue code.
func DefinitionFor(code Code) (Definition, bool) {
	item, ok := definitionsByCode[code]
	return item, ok
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
