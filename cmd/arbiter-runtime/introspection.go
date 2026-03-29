package main

import (
	"crypto/tls"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/arbiter/capability"
	"github.com/odvcencio/arbiter/internal/statusview"
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
	Issues       []statusview.Issue        `json:"issues"`
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
	issues := runtimeIssues(ready, reason, lastResult)
	return runtimeStatusPayload{
		Readiness: runtimeReadinessStatus{
			Ready:  ready,
			Reason: reason,
		},
		Issues: issues,
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

func runtimeIssues(ready bool, reason string, lastResult workflow.TickResult) []statusview.Issue {
	issues := make([]statusview.Issue, 0)
	if !ready && strings.TrimSpace(reason) != "" {
		issues = append(issues, statusview.Error("readiness", "runtime", "first_tick_incomplete", strings.TrimSpace(reason), true))
	}

	sourceKeys := make([]string, 0, len(lastResult.Sources))
	for key := range lastResult.Sources {
		sourceKeys = append(sourceKeys, key)
	}
	sort.Strings(sourceKeys)
	for _, key := range sourceKeys {
		item := lastResult.Sources[key]
		subject := runtimeIssueSubject(item.Alias, item.Target, key)
		switch {
		case !item.Available:
			issues = append(issues, statusview.Error("source", subject, "source_unavailable", runtimeIssueMessage("source unavailable", item.LastError), false))
		case item.ConsecutiveFailures > 0:
			issues = append(issues, statusview.Warning("source", subject, "source_failures", runtimeFailureMessage(item.ConsecutiveFailures, "consecutive source failures", item.LastError)))
		}
	}

	sinkKeys := make([]string, 0, len(lastResult.Sinks))
	for key := range lastResult.Sinks {
		sinkKeys = append(sinkKeys, key)
	}
	sort.Strings(sinkKeys)
	for _, key := range sinkKeys {
		item := lastResult.Sinks[key]
		subject := runtimeIssueSubject(item.Alias, item.Key, key)
		if !item.Available {
			issues = append(issues, statusview.Error("sink", subject, "sink_unavailable", runtimeIssueMessage("sink unavailable", item.LastError), false))
		} else if item.ConsecutiveFailures > 0 {
			issues = append(issues, statusview.Warning("sink", subject, "sink_failures", runtimeFailureMessage(item.ConsecutiveFailures, "consecutive sink failures", item.LastError)))
		}
		if item.Ambiguous > 0 {
			issues = append(issues, statusview.Warning("sink", subject, "sink_ambiguous", fmt.Sprintf("%d ambiguous deliveries", item.Ambiguous)))
		}
	}

	return issues
}

func runtimeIssueSubject(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return "unknown"
}

func runtimeIssueMessage(base string, detail string) string {
	base = strings.TrimSpace(base)
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return base
	}
	return base + ": " + detail
}

func runtimeFailureMessage(count int, noun string, detail string) string {
	message := fmt.Sprintf("%d %s", count, strings.TrimSpace(noun))
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return message
	}
	return message + ": " + detail
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
