package main

import (
	"context"

	arbiterv1 "github.com/odvcencio/arbiter/api/arbiter/v1"
	"github.com/odvcencio/arbiter/capability"
	"github.com/odvcencio/arbiter/workflow"
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

func protoRuntimeCapabilities(surface workflow.CapabilitySurface, manifest *capability.Manifest, control runtimeControlTransport, capabilityTransport runtimeCapabilityTransport) *arbiterv1.GetRuntimeCapabilitiesResponse {
	resp := &arbiterv1.GetRuntimeCapabilitiesResponse{
		Sources: make([]*arbiterv1.RuntimeSourceCapability, 0, len(surface.Sources)),
		Sinks:   make([]*arbiterv1.RuntimeHandlerCapability, 0, len(surface.Sinks)),
		Workers: make([]*arbiterv1.RuntimeHandlerCapability, 0, len(surface.Workers)),
		ControlTransport: &arbiterv1.RuntimeControlTransport{
			Enabled:          control.Enabled,
			Address:          control.Address,
			PublicListener:   control.PublicListener,
			AuthEnabled:      control.AuthEnabled,
			TlsEnabled:       control.TLSEnabled,
			MutualTlsEnabled: control.MutualTLSEnabled,
		},
		CapabilityTransport: &arbiterv1.RuntimeCapabilityTransport{
			Configured:  capabilityTransport.Configured,
			Target:      capabilityTransport.Target,
			AuthEnabled: capabilityTransport.AuthEnabled,
			TlsEnabled:  capabilityTransport.TLSEnabled,
			ServerName:  capabilityTransport.ServerName,
		},
	}
	for _, item := range surface.Sources {
		resp.Sources = append(resp.Sources, &arbiterv1.RuntimeSourceCapability{
			Scheme:      item.Scheme,
			Owner:       protoCapabilityOwner(item.Owner),
			Description: item.Description,
		})
	}
	for _, item := range surface.Sinks {
		resp.Sinks = append(resp.Sinks, &arbiterv1.RuntimeHandlerCapability{
			Kind:        item.Kind,
			Owner:       protoCapabilityOwner(item.Owner),
			Description: item.Description,
		})
	}
	for _, item := range surface.Workers {
		resp.Workers = append(resp.Workers, &arbiterv1.RuntimeHandlerCapability{
			Kind:        item.Kind,
			Owner:       protoCapabilityOwner(item.Owner),
			Description: item.Description,
		})
	}
	if manifest != nil {
		resp.Plugins = append(resp.Plugins, &arbiterv1.RuntimePluginInfo{
			Name:    manifest.Name,
			Version: manifest.Version,
		})
	}
	return resp
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
