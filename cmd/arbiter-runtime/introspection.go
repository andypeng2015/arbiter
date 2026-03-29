package main

import (
	"crypto/tls"
	"time"

	"github.com/odvcencio/arbiter/capability"
	"github.com/odvcencio/arbiter/workflow"
)

type runtimeControlTransport struct {
	Enabled          bool   `json:"enabled"`
	Address          string `json:"address,omitempty"`
	PublicListener   bool   `json:"public_listener,omitempty"`
	AuthEnabled      bool   `json:"auth_enabled"`
	TLSEnabled       bool   `json:"tls_enabled"`
	MutualTLSEnabled bool   `json:"mutual_tls_enabled"`
}

type runtimeCapabilityTransport struct {
	Configured  bool   `json:"configured"`
	Target      string `json:"target,omitempty"`
	AuthEnabled bool   `json:"auth_enabled"`
	TLSEnabled  bool   `json:"tls_enabled"`
	ServerName  string `json:"server_name,omitempty"`
}

type runtimeTransportStatus struct {
	Control    runtimeControlTransport    `json:"control"`
	Capability runtimeCapabilityTransport `json:"capability"`
}

type runtimeReadinessStatus struct {
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}

type runtimeSourceCapabilityStatus struct {
	Scheme      string `json:"scheme"`
	Owner       string `json:"owner"`
	Description string `json:"description,omitempty"`
}

type runtimeHandlerCapabilityStatus struct {
	Kind        string `json:"kind"`
	Owner       string `json:"owner"`
	Description string `json:"description,omitempty"`
}

type runtimePluginStatus struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type runtimeCapabilitiesStatus struct {
	Plugins []runtimePluginStatus            `json:"plugins,omitempty"`
	Sources []runtimeSourceCapabilityStatus  `json:"sources,omitempty"`
	Sinks   []runtimeHandlerCapabilityStatus `json:"sinks,omitempty"`
	Workers []runtimeHandlerCapabilityStatus `json:"workers,omitempty"`
}

type runtimeDeliveryStatus struct {
	Delivered int `json:"delivered,omitempty"`
	Enqueued  int `json:"enqueued,omitempty"`
	Retried   int `json:"retried,omitempty"`
}

type runtimeActivityStatus struct {
	Ticks        uint64                             `json:"ticks"`
	Errors       uint64                             `json:"errors"`
	LastTick     time.Time                          `json:"last_tick,omitempty"`
	Delivery     runtimeDeliveryStatus              `json:"delivery"`
	SourceStatus map[string]workflow.SourceSnapshot `json:"source_status,omitempty"`
	SinkStatus   map[string]workflow.SinkSnapshot   `json:"sink_status,omitempty"`
}

type runtimeStatusPayload struct {
	Readiness    runtimeReadinessStatus    `json:"readiness"`
	Transport    runtimeTransportStatus    `json:"transport"`
	Capabilities runtimeCapabilitiesStatus `json:"capabilities"`
	Activity     runtimeActivityStatus     `json:"activity"`

	Ready             bool                               `json:"ready"`
	Ticks             uint64                             `json:"ticks"`
	Errors            uint64                             `json:"errors"`
	LastTick          time.Time                          `json:"last_tick,omitempty"`
	Sources           map[string]workflow.SourceSnapshot `json:"sources,omitempty"`
	Sinks             map[string]workflow.SinkSnapshot   `json:"sinks,omitempty"`
	Delivered         int                                `json:"delivered,omitempty"`
	Enqueued          int                                `json:"enqueued,omitempty"`
	Retried           int                                `json:"retried,omitempty"`
	CapabilityPlugins []runtimePluginStatus              `json:"capability_plugins,omitempty"`
}

func newRuntimeControlTransport(address string, tokens []string, tlsConfig *tls.Config, publicListener bool) runtimeControlTransport {
	status := runtimeControlTransport{
		Enabled:        address != "",
		Address:        address,
		PublicListener: publicListener,
		AuthEnabled:    len(tokens) > 0,
		TLSEnabled:     tlsConfig != nil,
	}
	if tlsConfig != nil && tlsConfig.ClientAuth == tls.RequireAndVerifyClientCert {
		status.MutualTLSEnabled = true
	}
	return status
}

func newRuntimeCapabilityTransport(target string, authEnabled bool, tlsEnabled bool, serverName string) runtimeCapabilityTransport {
	return runtimeCapabilityTransport{
		Configured:  target != "",
		Target:      target,
		AuthEnabled: authEnabled,
		TLSEnabled:  tlsEnabled,
		ServerName:  serverName,
	}
}

func newRuntimeStatusPayload(
	ready bool,
	tickCount uint64,
	errorCount uint64,
	lastTick time.Time,
	lastResult workflow.TickResult,
	surface workflow.CapabilitySurface,
	manifest *capability.Manifest,
	control runtimeControlTransport,
	capabilityTransport runtimeCapabilityTransport,
) runtimeStatusPayload {
	reason := ""
	if !ready {
		reason = "first tick incomplete"
	}
	caps := capabilityStatus(surface, manifest)
	sources := cloneSourceStatus(lastResult.Sources)
	sinks := cloneSinkStatus(lastResult.Sinks)
	return runtimeStatusPayload{
		Readiness: runtimeReadinessStatus{
			Ready:  ready,
			Reason: reason,
		},
		Transport: runtimeTransportStatus{
			Control:    control,
			Capability: capabilityTransport,
		},
		Capabilities: caps,
		Activity: runtimeActivityStatus{
			Ticks:    tickCount,
			Errors:   errorCount,
			LastTick: lastTick,
			Delivery: runtimeDeliveryStatus{
				Delivered: lastResult.Delivered,
				Enqueued:  lastResult.Enqueued,
				Retried:   lastResult.Retried,
			},
			SourceStatus: sources,
			SinkStatus:   sinks,
		},
		Ready:             ready,
		Ticks:             tickCount,
		Errors:            errorCount,
		LastTick:          lastTick,
		Sources:           sources,
		Sinks:             sinks,
		Delivered:         lastResult.Delivered,
		Enqueued:          lastResult.Enqueued,
		Retried:           lastResult.Retried,
		CapabilityPlugins: caps.Plugins,
	}
}

func capabilityStatus(surface workflow.CapabilitySurface, manifest *capability.Manifest) runtimeCapabilitiesStatus {
	sources := make([]runtimeSourceCapabilityStatus, 0, len(surface.Sources))
	for _, item := range surface.Sources {
		sources = append(sources, runtimeSourceCapabilityStatus{
			Scheme:      item.Scheme,
			Owner:       string(item.Owner),
			Description: item.Description,
		})
	}

	sinks := make([]runtimeHandlerCapabilityStatus, 0, len(surface.Sinks))
	for _, item := range surface.Sinks {
		sinks = append(sinks, runtimeHandlerCapabilityStatus{
			Kind:        item.Kind,
			Owner:       string(item.Owner),
			Description: item.Description,
		})
	}

	workers := make([]runtimeHandlerCapabilityStatus, 0, len(surface.Workers))
	for _, item := range surface.Workers {
		workers = append(workers, runtimeHandlerCapabilityStatus{
			Kind:        item.Kind,
			Owner:       string(item.Owner),
			Description: item.Description,
		})
	}

	return runtimeCapabilitiesStatus{
		Plugins: capabilityPluginsStatus(manifest),
		Sources: sources,
		Sinks:   sinks,
		Workers: workers,
	}
}

func capabilityPluginsStatus(manifest *capability.Manifest) []runtimePluginStatus {
	if manifest == nil {
		return nil
	}
	return []runtimePluginStatus{{
		Name:    manifest.Name,
		Version: manifest.Version,
	}}
}

func cloneSourceStatus(in map[string]workflow.SourceSnapshot) map[string]workflow.SourceSnapshot {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]workflow.SourceSnapshot, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneSinkStatus(in map[string]workflow.SinkSnapshot) map[string]workflow.SinkSnapshot {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]workflow.SinkSnapshot, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
