package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/arbiter/dataplane"
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
		Issues:    agentIssues(status, reason, policy),
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

func agentIssues(status dataplane.AgentStatus, reason string, policy readinessPolicy) []statusview.Issue {
	issues := make([]statusview.Issue, 0)
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		issues = append(issues, statusview.Error("readiness", "agent", agentReadinessCode(trimmed), trimmed, true))
	}
	if trimmed := strings.TrimSpace(status.LastUpstreamError); trimmed != "" {
		issues = append(issues, statusview.Warning("upstream", "control-plane", "upstream_error", trimmed))
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
				issues = append(issues, statusview.Error("sync", subject, "bundle_never_synced", "bundle has never synced", true))
			case item.StalenessMs > limitMs:
				issues = append(issues, statusview.Error("sync", subject, "bundle_stale", fmt.Sprintf("bundle stale (%dms > %dms)", item.StalenessMs, limitMs), true))
			}
			if item.OverrideConfigured && !item.OverrideSyncedAt.IsZero() && item.OverrideStalenessMs > limitMs {
				issues = append(issues, statusview.Error("sync", subject, "override_stale", fmt.Sprintf("overrides stale (%dms > %dms)", item.OverrideStalenessMs, limitMs), true))
			}
		}
		if !item.BundleWatchConnected {
			issues = append(issues, statusview.Warning("sync", subject, "bundle_watch_disconnected", "bundle watch disconnected"))
		}
		if item.OverrideConfigured && !item.OverrideWatchConnected {
			issues = append(issues, statusview.Warning("sync", subject, "override_watch_disconnected", "override watch disconnected"))
		}
		if trimmed := strings.TrimSpace(item.LastBundleError); trimmed != "" {
			issues = append(issues, statusview.Warning("sync", subject, "bundle_sync_error", trimmed))
		}
		if trimmed := strings.TrimSpace(item.LastOverrideError); trimmed != "" {
			issues = append(issues, statusview.Warning("sync", subject, "override_sync_error", trimmed))
		}
	}
	return issues
}

func agentReadinessCode(reason string) string {
	switch strings.TrimSpace(reason) {
	case "status unavailable":
		return "status_unavailable"
	case "initial sync incomplete":
		return "initial_sync_incomplete"
	default:
		if strings.Contains(reason, " has never synced") {
			return "bundle_never_synced"
		}
		if strings.Contains(reason, " overrides stale ") {
			return "override_stale"
		}
		if strings.Contains(reason, " stale ") {
			return "bundle_stale"
		}
		return "not_ready"
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
