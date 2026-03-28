package main

import (
	"testing"

	"github.com/odvcencio/arbiter/capability"
	"github.com/odvcencio/arbiter/workflow"
)

func TestCapabilityStatus(t *testing.T) {
	status := capabilityStatus(workflow.CapabilitySurface{
		Sources: []workflow.SourceCapability{{
			Scheme:      "kafka",
			Owner:       workflow.CapabilityOwnerPlugin,
			Description: "stream facts",
		}},
		Sinks: []workflow.HandlerCapability{{
			Kind:        "discord",
			Owner:       workflow.CapabilityOwnerHost,
			Description: "discord sink",
		}},
		Workers: []workflow.HandlerCapability{{
			Kind:        "python",
			Owner:       workflow.CapabilityOwnerPlugin,
			Description: "python worker",
		}},
	})

	sources := status["sources"].([]map[string]any)
	if len(sources) != 1 || sources[0]["scheme"] != "kafka" || sources[0]["owner"] != "plugin" {
		t.Fatalf("unexpected sources: %+v", sources)
	}

	sinks := status["sinks"].([]map[string]any)
	if len(sinks) != 1 || sinks[0]["kind"] != "discord" || sinks[0]["owner"] != "host" {
		t.Fatalf("unexpected sinks: %+v", sinks)
	}

	workers := status["workers"].([]map[string]any)
	if len(workers) != 1 || workers[0]["kind"] != "python" || workers[0]["owner"] != "plugin" {
		t.Fatalf("unexpected workers: %+v", workers)
	}
}

func TestCapabilityPluginsStatus(t *testing.T) {
	status := capabilityPluginsStatus(&capability.Manifest{
		Name:    "ops-plugin",
		Version: "1.2.3",
	})
	if len(status) != 1 {
		t.Fatalf("status len = %d, want 1", len(status))
	}
	if status[0]["name"] != "ops-plugin" || status[0]["version"] != "1.2.3" {
		t.Fatalf("unexpected plugin status: %+v", status[0])
	}
}
