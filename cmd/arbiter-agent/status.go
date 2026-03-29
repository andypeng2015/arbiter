package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/odvcencio/arbiter/dataplane"
)

type readinessPolicy struct {
	maxStaleness time.Duration
}

type agentStatusPayload struct {
	Readiness agentReadinessStatus `json:"readiness"`
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
