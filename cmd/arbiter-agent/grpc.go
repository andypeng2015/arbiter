package main

import (
	"context"
	"time"

	arbiterv1 "github.com/odvcencio/arbiter/api/arbiter/v1"
	"github.com/odvcencio/arbiter/dataplane"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type agentRPCServer struct {
	arbiterv1.UnimplementedAgentServiceServer
	syncer    *dataplane.Agent
	policy    readinessPolicy
	transport agentTransportStatus
}

func newAgentRPCServer(syncer *dataplane.Agent, policy readinessPolicy, transport agentTransportStatus) *agentRPCServer {
	return &agentRPCServer{
		syncer:    syncer,
		policy:    policy,
		transport: transport,
	}
}

func (s *agentRPCServer) GetAgentStatus(context.Context, *arbiterv1.GetAgentStatusRequest) (*arbiterv1.GetAgentStatusResponse, error) {
	if s == nil || s.syncer == nil {
		return &arbiterv1.GetAgentStatusResponse{}, nil
	}
	status, reason := readinessStatus(s.syncer, s.policy)
	return protoAgentStatus(newAgentStatusPayload(status, reason, s.policy, s.transport)), nil
}

func protoAgentStatus(payload agentStatusPayload) *arbiterv1.GetAgentStatusResponse {
	return &arbiterv1.GetAgentStatusResponse{
		Readiness: &arbiterv1.AgentReadinessStatus{
			Ready:          payload.Readiness.Ready,
			Reason:         payload.Readiness.Reason,
			MaxStalenessMs: payload.Readiness.MaxStalenessMs,
			TargetCount:    uint32(payload.Readiness.TargetCount),
			ReadyCount:     uint32(payload.Readiness.ReadyCount),
		},
		Transport: &arbiterv1.AgentTransportStatus{
			Control: &arbiterv1.AgentControlTransport{
				Enabled:          payload.Transport.Control.Enabled,
				Address:          payload.Transport.Control.Address,
				PublicListener:   payload.Transport.Control.PublicListener,
				AuthEnabled:      payload.Transport.Control.AuthEnabled,
				TlsEnabled:       payload.Transport.Control.TLSEnabled,
				MutualTlsEnabled: payload.Transport.Control.MutualTLSEnabled,
			},
			Upstream: &arbiterv1.AgentUpstreamTransport{
				Configured:  payload.Transport.Upstream.Configured,
				Target:      payload.Transport.Upstream.Target,
				AuthEnabled: payload.Transport.Upstream.AuthEnabled,
				TlsEnabled:  payload.Transport.Upstream.TLSEnabled,
				ServerName:  payload.Transport.Upstream.ServerName,
			},
		},
		Sync: &arbiterv1.AgentSyncStatus{
			PrimaryName:             payload.Sync.PrimaryName,
			BundleErrorsTotal:       payload.Sync.BundleErrorsTotal,
			OverrideErrorsTotal:     payload.Sync.OverrideErrorsTotal,
			BundleReconnectsTotal:   payload.Sync.BundleReconnectsTotal,
			OverrideReconnectsTotal: payload.Sync.OverrideReconnectsTotal,
			LastUpstreamError:       payload.Sync.LastUpstreamError,
			LastUpstreamErrorAt:     protoAgentTimestamp(payload.Sync.LastUpstreamErrorAt),
			Bundles:                 protoAgentBundles(payload.Sync.Bundles),
		},
	}
}

func protoAgentBundles(items []dataplane.BundleSyncStatus) []*arbiterv1.AgentBundleSyncStatus {
	if len(items) == 0 {
		return nil
	}
	out := make([]*arbiterv1.AgentBundleSyncStatus, 0, len(items))
	for _, item := range items {
		out = append(out, &arbiterv1.AgentBundleSyncStatus{
			Name:                   item.Name,
			BundleId:               item.BundleID,
			Checksum:               item.Checksum,
			LoadedAt:               protoAgentTimestamp(item.LoadedAt),
			BundleSyncedAt:         protoAgentTimestamp(item.BundleSyncedAt),
			OverrideSyncedAt:       protoAgentTimestamp(item.OverrideSyncedAt),
			StalenessMs:            item.StalenessMs,
			OverrideStalenessMs:    item.OverrideStalenessMs,
			BundleWatchConnected:   item.BundleWatchConnected,
			OverrideConfigured:     item.OverrideConfigured,
			OverrideWatchConnected: item.OverrideWatchConnected,
			BundleErrorsTotal:      item.BundleErrorsTotal,
			OverrideErrorsTotal:    item.OverrideErrorsTotal,
			BundleReconnects:       item.BundleReconnects,
			OverrideReconnects:     item.OverrideReconnects,
			LastBundleError:        item.LastBundleError,
			LastBundleErrorAt:      protoAgentTimestamp(item.LastBundleErrorAt),
			LastOverrideError:      item.LastOverrideError,
			LastOverrideErrorAt:    protoAgentTimestamp(item.LastOverrideErrorAt),
		})
	}
	return out
}

func protoAgentTimestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}
