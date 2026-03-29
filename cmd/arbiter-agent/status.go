package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/arbiter/dataplane"
	"github.com/odvcencio/arbiter/internal/grpcutil"
	"github.com/odvcencio/arbiter/internal/statusview"
)

type readinessPolicy struct {
	maxStaleness time.Duration
}

type agentStatusPayload struct {
	Readiness agentReadinessStatus `json:"readiness"`
	Issues    []statusview.Issue   `json:"issues"`
	Transport agentTransportStatus `json:"transport"`
	Sync      agentSyncStatus      `json:"sync"`

	Ready                   bool                         `json:"ready"`
	PrimaryName             string                       `json:"primary_name,omitempty"`
	TargetCount             int                          `json:"target_count"`
	ReadyCount              int                          `json:"ready_count"`
	BundleErrorsTotal       int64                        `json:"bundle_errors_total"`
	OverrideErrorsTotal     int64                        `json:"override_errors_total"`
	BundleReconnectsTotal   int64                        `json:"bundle_reconnects_total"`
	OverrideReconnectsTotal int64                        `json:"override_reconnects_total"`
	LastUpstreamError       string                       `json:"last_upstream_error,omitempty"`
	LastUpstreamErrorAt     time.Time                    `json:"last_upstream_error_at,omitempty"`
	Bundles                 []dataplane.BundleSyncStatus `json:"bundles,omitempty"`
}

func newStatusHandler(syncer *dataplane.Agent, policy readinessPolicy, transport agentTransportStatus) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		_, reason := readinessStatus(syncer, policy)
		if reason != "" {
			http.Error(w, reason, http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		if syncer == nil {
			http.Error(w, "status unavailable", http.StatusServiceUnavailable)
			return
		}
		status, reason := readinessStatus(syncer, policy)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(newAgentStatusPayload(status, reason, policy, transport))
	})
	mux.HandleFunc("/status/issues", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(statusview.DefinitionsForSurface(statusview.SurfaceAgent))
	})
	return mux
}

func newAgentStatusPayload(status dataplane.AgentStatus, reason string, policy readinessPolicy, transport agentTransportStatus) agentStatusPayload {
	return agentStatusPayload{
		Readiness: agentReadinessStatus{
			Ready:          reason == "",
			Reason:         reason,
			MaxStalenessMs: policy.maxStaleness.Milliseconds(),
			TargetCount:    status.TargetCount,
			ReadyCount:     status.ReadyCount,
		},
		Issues:    agentIssues(status, reason, policy, transport),
		Transport: transport,
		Sync: agentSyncStatus{
			PrimaryName:             status.PrimaryName,
			BundleErrorsTotal:       status.BundleErrorsTotal,
			OverrideErrorsTotal:     status.OverrideErrorsTotal,
			BundleReconnectsTotal:   status.BundleReconnectsTotal,
			OverrideReconnectsTotal: status.OverrideReconnectsTotal,
			LastUpstreamError:       status.LastUpstreamError,
			LastUpstreamErrorAt:     status.LastUpstreamErrorAt,
			Bundles:                 append([]dataplane.BundleSyncStatus(nil), status.Bundles...),
		},
		Ready:                   status.Ready,
		PrimaryName:             status.PrimaryName,
		TargetCount:             status.TargetCount,
		ReadyCount:              status.ReadyCount,
		BundleErrorsTotal:       status.BundleErrorsTotal,
		OverrideErrorsTotal:     status.OverrideErrorsTotal,
		BundleReconnectsTotal:   status.BundleReconnectsTotal,
		OverrideReconnectsTotal: status.OverrideReconnectsTotal,
		LastUpstreamError:       status.LastUpstreamError,
		LastUpstreamErrorAt:     status.LastUpstreamErrorAt,
		Bundles:                 append([]dataplane.BundleSyncStatus(nil), status.Bundles...),
	}
}

func agentIssues(status dataplane.AgentStatus, reason string, policy readinessPolicy, transport agentTransportStatus) []statusview.Issue {
	issues := make([]statusview.Issue, 0)
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		issues = append(issues, statusview.New(agentReadinessCode(trimmed), "agent", trimmed))
	}
	if transport.Control.PublicListener && !transport.Control.TLSEnabled && !transport.Control.AuthEnabled {
		issues = append(issues, statusview.New(statusview.CodePublicControlInsecure, agentTransportSubject(transport.Control.Address, "agent-control"), "public control listener has no TLS or auth"))
	}
	if transport.Upstream.Configured && grpcutil.IsPublicListenAddr(transport.Upstream.Target) && !transport.Upstream.TLSEnabled && !transport.Upstream.AuthEnabled {
		issues = append(issues, statusview.New(statusview.CodeUpstreamTransportInsecure, agentTransportSubject(transport.Upstream.Target, "upstream"), "upstream transport has no TLS or auth"))
	}
	if trimmed := strings.TrimSpace(status.LastUpstreamError); trimmed != "" {
		issues = append(issues, statusview.New(statusview.CodeUpstreamError, "control-plane", trimmed))
	}

	limitMs := policy.maxStaleness.Milliseconds()
	items := append([]dataplane.BundleSyncStatus(nil), status.Bundles...)
	sort.Slice(items, func(i, j int) bool {
		left := agentIssueSubject(items[i])
		right := agentIssueSubject(items[j])
		if left == right {
			return items[i].BundleID < items[j].BundleID
		}
		return left < right
	})
	for _, item := range items {
		subject := agentIssueSubject(item)
		if limitMs > 0 {
			switch {
			case item.BundleSyncedAt.IsZero():
				issues = append(issues, statusview.New(statusview.CodeBundleNeverSynced, subject, "bundle has never synced"))
			case item.StalenessMs > limitMs:
				issues = append(issues, statusview.New(statusview.CodeBundleStale, subject, fmt.Sprintf("bundle stale (%dms > %dms)", item.StalenessMs, limitMs)))
			}
			if item.OverrideConfigured && !item.OverrideSyncedAt.IsZero() && item.OverrideStalenessMs > limitMs {
				issues = append(issues, statusview.New(statusview.CodeOverrideStale, subject, fmt.Sprintf("overrides stale (%dms > %dms)", item.OverrideStalenessMs, limitMs)))
			}
		}
		if !item.BundleWatchConnected {
			issues = append(issues, statusview.New(statusview.CodeBundleWatchDisconnected, subject, "bundle watch disconnected"))
		}
		if item.OverrideConfigured && !item.OverrideWatchConnected {
			issues = append(issues, statusview.New(statusview.CodeOverrideWatchDisconnected, subject, "override watch disconnected"))
		}
		if trimmed := strings.TrimSpace(item.LastBundleError); trimmed != "" {
			issues = append(issues, statusview.New(statusview.CodeBundleSyncError, subject, trimmed))
		}
		if trimmed := strings.TrimSpace(item.LastOverrideError); trimmed != "" {
			issues = append(issues, statusview.New(statusview.CodeOverrideSyncError, subject, trimmed))
		}
	}
	return issues
}

func agentReadinessCode(reason string) statusview.Code {
	switch strings.TrimSpace(reason) {
	case "status unavailable":
		return statusview.CodeStatusUnavailable
	case "initial sync incomplete":
		return statusview.CodeInitialSyncIncomplete
	default:
		if strings.Contains(reason, " has never synced") {
			return statusview.CodeBundleNeverSynced
		}
		if strings.Contains(reason, " overrides stale ") {
			return statusview.CodeOverrideStale
		}
		if strings.Contains(reason, " stale ") {
			return statusview.CodeBundleStale
		}
		return statusview.CodeNotReady
	}
}

func agentIssueSubject(item dataplane.BundleSyncStatus) string {
	if value := strings.TrimSpace(item.Name); value != "" {
		return value
	}
	if value := strings.TrimSpace(item.BundleID); value != "" {
		return value
	}
	return "unknown"
}

func agentTransportSubject(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return "unknown"
}

func readinessStatus(syncer *dataplane.Agent, policy readinessPolicy) (dataplane.AgentStatus, string) {
	if syncer == nil {
		return dataplane.AgentStatus{}, "status unavailable"
	}
	status := syncer.Status()
	if !status.Ready {
		return status, "initial sync incomplete"
	}
	if policy.maxStaleness <= 0 {
		return status, ""
	}

	limitMs := policy.maxStaleness.Milliseconds()
	for _, bundle := range status.Bundles {
		name := bundle.Name
		if name == "" {
			name = bundle.BundleID
		}
		if bundle.BundleSyncedAt.IsZero() {
			return status, fmt.Sprintf("bundle %s has never synced", name)
		}
		if bundle.StalenessMs > limitMs {
			return status, fmt.Sprintf("bundle %s stale (%dms > %dms)", name, bundle.StalenessMs, limitMs)
		}
		if !bundle.OverrideSyncedAt.IsZero() && bundle.OverrideStalenessMs > limitMs {
			return status, fmt.Sprintf("bundle %s overrides stale (%dms > %dms)", name, bundle.OverrideStalenessMs, limitMs)
		}
	}
	return status, ""
}
