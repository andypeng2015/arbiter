package main

import (
	"context"
	"sort"
	"time"

	arbiterv1 "github.com/odvcencio/arbiter/api/arbiter/v1"
	"github.com/odvcencio/arbiter/capability"
	"github.com/odvcencio/arbiter/internal/statusview"
	"github.com/odvcencio/arbiter/workflow"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type runtimeRPCServer struct {
	arbiterv1.UnimplementedRuntimeServiceServer
	rt *runtime
}

func newRuntimeRPCServer(rt *runtime) *runtimeRPCServer {
	return &runtimeRPCServer{rt: rt}
}

func (s *runtimeRPCServer) GetRuntimeCapabilities(context.Context, *arbiterv1.GetRuntimeCapabilitiesRequest) (*arbiterv1.GetRuntimeCapabilitiesResponse, error) {
	if s == nil || s.rt == nil || s.rt.runner == nil {
		return &arbiterv1.GetRuntimeCapabilitiesResponse{}, nil
	}
	return protoRuntimeCapabilities(s.rt.runner.Capabilities(), s.rt.caps, s.rt.controlTransport, s.rt.capabilityTransport), nil
}

func (s *runtimeRPCServer) GetRuntimeStatus(context.Context, *arbiterv1.GetRuntimeStatusRequest) (*arbiterv1.GetRuntimeStatusResponse, error) {
	if s == nil || s.rt == nil || s.rt.runner == nil {
		return &arbiterv1.GetRuntimeStatusResponse{}, nil
	}
	s.rt.mu.RLock()
	payload := newRuntimeStatusPayload(
		s.rt.ready,
		s.rt.tickCount,
		s.rt.errors,
		s.rt.lastTick,
		s.rt.lastResult,
		s.rt.runner.Capabilities(),
		s.rt.caps,
		s.rt.controlTransport,
		s.rt.capabilityTransport,
	)
	s.rt.mu.RUnlock()
	return protoRuntimeStatus(payload), nil
}

func (*runtimeRPCServer) GetStatusIssueCatalog(context.Context, *arbiterv1.GetStatusIssueCatalogRequest) (*arbiterv1.GetStatusIssueCatalogResponse, error) {
	return &arbiterv1.GetStatusIssueCatalogResponse{Definitions: statusview.ProtoDefinitions()}, nil
}

func protoRuntimeCapabilities(surface workflow.CapabilitySurface, manifest *capability.Manifest, control runtimeControlTransport, capabilityTransport runtimeCapabilityTransport) *arbiterv1.GetRuntimeCapabilitiesResponse {
	capabilities := protoRuntimeCapabilitiesStatus(capabilityStatus(surface, manifest))
	transport := protoRuntimeTransportStatus(control, capabilityTransport)
	resp := &arbiterv1.GetRuntimeCapabilitiesResponse{
		Sources:             capabilities.GetSources(),
		Sinks:               capabilities.GetSinks(),
		Workers:             capabilities.GetWorkers(),
		Plugins:             capabilities.GetPlugins(),
		ControlTransport:    transport.GetControl(),
		CapabilityTransport: transport.GetCapability(),
	}
	return resp
}

func protoRuntimeStatus(payload runtimeStatusPayload) *arbiterv1.GetRuntimeStatusResponse {
	return &arbiterv1.GetRuntimeStatusResponse{
		Readiness: &arbiterv1.RuntimeReadinessStatus{
			Ready:  payload.Readiness.Ready,
			Reason: payload.Readiness.Reason,
		},
		Issues:       statusview.ProtoIssues(payload.Issues),
		Transport:    protoRuntimeTransportStatus(payload.Transport.Control, payload.Transport.Capability),
		Capabilities: protoRuntimeCapabilitiesStatus(payload.Capabilities),
		Activity: &arbiterv1.RuntimeActivityStatus{
			Ticks:    payload.Activity.Ticks,
			Errors:   payload.Activity.Errors,
			LastTick: protoTimestamp(payload.Activity.LastTick),
			Delivery: &arbiterv1.RuntimeDeliveryStatus{
				Delivered: uint64(payload.Activity.Delivery.Delivered),
				Enqueued:  uint64(payload.Activity.Delivery.Enqueued),
				Retried:   uint64(payload.Activity.Delivery.Retried),
			},
			SourceStatus: protoRuntimeSourceStatuses(payload.Activity.SourceStatus),
			SinkStatus:   protoRuntimeSinkStatuses(payload.Activity.SinkStatus),
		},
	}
}

func protoRuntimeTransportStatus(control runtimeControlTransport, capabilityTransport runtimeCapabilityTransport) *arbiterv1.RuntimeTransportStatus {
	return &arbiterv1.RuntimeTransportStatus{
		Control: &arbiterv1.RuntimeControlTransport{
			Enabled:          control.Enabled,
			Address:          control.Address,
			PublicListener:   control.PublicListener,
			AuthEnabled:      control.AuthEnabled,
			TlsEnabled:       control.TLSEnabled,
			MutualTlsEnabled: control.MutualTLSEnabled,
		},
		Capability: &arbiterv1.RuntimeCapabilityTransport{
			Configured:  capabilityTransport.Configured,
			Target:      capabilityTransport.Target,
			AuthEnabled: capabilityTransport.AuthEnabled,
			TlsEnabled:  capabilityTransport.TLSEnabled,
			ServerName:  capabilityTransport.ServerName,
		},
	}
}

func protoRuntimeCapabilitiesStatus(status runtimeCapabilitiesStatus) *arbiterv1.RuntimeCapabilitiesStatus {
	resp := &arbiterv1.RuntimeCapabilitiesStatus{
		Plugins: make([]*arbiterv1.RuntimePluginInfo, 0, len(status.Plugins)),
		Sources: make([]*arbiterv1.RuntimeSourceCapability, 0, len(status.Sources)),
		Sinks:   make([]*arbiterv1.RuntimeHandlerCapability, 0, len(status.Sinks)),
		Workers: make([]*arbiterv1.RuntimeHandlerCapability, 0, len(status.Workers)),
	}
	for _, item := range status.Plugins {
		resp.Plugins = append(resp.Plugins, &arbiterv1.RuntimePluginInfo{
			Name:    item.Name,
			Version: item.Version,
		})
	}
	for _, item := range status.Sources {
		resp.Sources = append(resp.Sources, &arbiterv1.RuntimeSourceCapability{
			Scheme:      item.Scheme,
			Owner:       protoCapabilityOwner(workflow.CapabilityOwner(item.Owner)),
			Description: item.Description,
		})
	}
	for _, item := range status.Sinks {
		resp.Sinks = append(resp.Sinks, &arbiterv1.RuntimeHandlerCapability{
			Kind:        item.Kind,
			Owner:       protoCapabilityOwner(workflow.CapabilityOwner(item.Owner)),
			Description: item.Description,
		})
	}
	for _, item := range status.Workers {
		resp.Workers = append(resp.Workers, &arbiterv1.RuntimeHandlerCapability{
			Kind:        item.Kind,
			Owner:       protoCapabilityOwner(workflow.CapabilityOwner(item.Owner)),
			Description: item.Description,
		})
	}
	return resp
}

func protoRuntimeSourceStatuses(items map[string]workflow.SourceSnapshot) []*arbiterv1.RuntimeSourceStatus {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*arbiterv1.RuntimeSourceStatus, 0, len(keys))
	for _, key := range keys {
		item := items[key]
		out = append(out, &arbiterv1.RuntimeSourceStatus{
			Target:              item.Target,
			Alias:               item.Alias,
			Available:           item.Available,
			FactCount:           uint32(item.FactCount),
			ConsecutiveFailures: uint32(item.ConsecutiveFailures),
			LastError:           item.LastError,
			LastAttemptAt:       protoTimestamp(item.LastAttemptAt),
			LastSuccessAt:       protoTimestamp(item.LastSuccessAt),
			NextRetryAt:         protoTimestamp(item.NextRetryAt),
		})
	}
	return out
}

func protoRuntimeSinkStatuses(items map[string]workflow.SinkSnapshot) []*arbiterv1.RuntimeSinkStatus {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*arbiterv1.RuntimeSinkStatus, 0, len(keys))
	for _, key := range keys {
		item := items[key]
		out = append(out, &arbiterv1.RuntimeSinkStatus{
			Key:                 item.Key,
			Alias:               item.Alias,
			Kind:                item.Kind,
			Target:              item.Target,
			Available:           item.Available,
			Pending:             uint32(item.Pending),
			Ambiguous:           uint32(item.Ambiguous),
			ConsecutiveFailures: uint32(item.ConsecutiveFailures),
			LastError:           item.LastError,
			LastAttemptAt:       protoTimestamp(item.LastAttemptAt),
			LastSuccessAt:       protoTimestamp(item.LastSuccessAt),
			NextRetryAt:         protoTimestamp(item.NextRetryAt),
		})
	}
	return out
}

func protoTimestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func protoCapabilityOwner(owner workflow.CapabilityOwner) arbiterv1.CapabilityOwner {
	switch owner {
	case workflow.CapabilityOwnerCore:
		return arbiterv1.CapabilityOwner_CAPABILITY_OWNER_CORE
	case workflow.CapabilityOwnerHost:
		return arbiterv1.CapabilityOwner_CAPABILITY_OWNER_HOST
	case workflow.CapabilityOwnerPlugin:
		return arbiterv1.CapabilityOwner_CAPABILITY_OWNER_PLUGIN
	default:
		return arbiterv1.CapabilityOwner_CAPABILITY_OWNER_UNSPECIFIED
	}
}
