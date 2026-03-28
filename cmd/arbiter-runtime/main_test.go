package main

import (
	"testing"

	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/capability"
)

func TestCapabilityStatus(t *testing.T) {
	status := capabilityStatus(&capability.Manifest{
		Name:    "ops-plugin",
		Version: "1.2.3",
		Sources: map[string]capability.SourceRegistration{
			"kafka": {Scheme: "kafka", Description: "stream facts"},
		},
		Sinks: map[arbiter.ArbiterHandlerKind]capability.SinkRegistration{
			"discord": {Kind: "discord", Description: "discord sink"},
		},
		Workers: map[arbiter.ArbiterHandlerKind]capability.WorkerRegistration{
			"python": {Kind: "python", Description: "python worker"},
		},
	})

	if status["name"] != "ops-plugin" || status["version"] != "1.2.3" {
		t.Fatalf("unexpected manifest header: %+v", status)
	}

	sources := status["sources"].([]map[string]any)
	if len(sources) != 1 || sources[0]["scheme"] != "kafka" {
		t.Fatalf("unexpected sources: %+v", sources)
	}

	sinks := status["sinks"].([]map[string]any)
	if len(sinks) != 1 || sinks[0]["kind"] != "discord" {
		t.Fatalf("unexpected sinks: %+v", sinks)
	}

	workers := status["workers"].([]map[string]any)
	if len(workers) != 1 || workers[0]["kind"] != "python" {
		t.Fatalf("unexpected workers: %+v", workers)
	}
}
