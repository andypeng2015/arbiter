package workflow

import (
	"slices"
	"strings"

	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/expert/factsource"
)

// CapabilityOwner identifies who owns one runtime transport surface.
type CapabilityOwner string

const (
	CapabilityOwnerCore   CapabilityOwner = "core"
	CapabilityOwnerHost   CapabilityOwner = "host"
	CapabilityOwnerPlugin CapabilityOwner = "plugin"
)

// SourceCapabilitySpec describes one inspectable source target form.
type SourceCapabilitySpec struct {
	Owner       CapabilityOwner
	Description string
}

// HandlerCapabilitySpec describes one inspectable sink or worker runtime kind.
type HandlerCapabilitySpec struct {
	Owner       CapabilityOwner
	Description string
}

// SourceCapability is one visible source target form the runtime can load.
type SourceCapability struct {
	Scheme      string          `json:"scheme"`
	Owner       CapabilityOwner `json:"owner"`
	Description string          `json:"description,omitempty"`
}

// HandlerCapability is one visible sink or worker runtime kind.
type HandlerCapability struct {
	Kind        string          `json:"kind"`
	Owner       CapabilityOwner `json:"owner"`
	Description string          `json:"description,omitempty"`
}

// CapabilitySurface is the runtime's inspectable capability algebra.
type CapabilitySurface struct {
	Sources []SourceCapability  `json:"sources,omitempty"`
	Sinks   []HandlerCapability `json:"sinks,omitempty"`
	Workers []HandlerCapability `json:"workers,omitempty"`
}

func buildCapabilitySurface(opts RunnerOptions, usesDefaultLoader bool) CapabilitySurface {
	sourceMap := make(map[string]SourceCapability)
	sinkMap := make(map[string]HandlerCapability)
	workerMap := make(map[string]HandlerCapability)

	addSource := func(scheme string, spec SourceCapabilitySpec) {
		scheme = normalizeCapabilityKey(scheme)
		if scheme == "" {
			return
		}
		current := sourceMap[scheme]
		current.Scheme = scheme
		if current.Owner == "" {
			current.Owner = normalizeCapabilityOwner(spec.Owner, CapabilityOwnerHost)
		}
		if current.Description == "" {
			current.Description = strings.TrimSpace(spec.Description)
		}
		sourceMap[scheme] = current
	}

	addHandler := func(dst map[string]HandlerCapability, kind arbiter.ArbiterHandlerKind, spec HandlerCapabilitySpec) {
		key := normalizeCapabilityKey(string(kind))
		if key == "" {
			return
		}
		current := dst[key]
		current.Kind = key
		if current.Owner == "" {
			current.Owner = normalizeCapabilityOwner(spec.Owner, CapabilityOwnerHost)
		}
		if current.Description == "" {
			current.Description = strings.TrimSpace(spec.Description)
		}
		dst[key] = current
	}

	addSource("chain", SourceCapabilitySpec{
		Owner:       CapabilityOwnerCore,
		Description: "Runtime-owned chained arbiter facts (chain://...)",
	})
	addSource("worker", SourceCapabilitySpec{
		Owner:       CapabilityOwnerCore,
		Description: "Runtime-owned worker result facts (worker://...)",
	})
	if usesDefaultLoader {
		for _, scheme := range factsource.Schemes() {
			addSource(scheme, SourceCapabilitySpec{Owner: CapabilityOwnerCore})
		}
	}
	for scheme, spec := range opts.SourceCapabilities {
		addSource(scheme, spec)
	}

	addHandler(sinkMap, arbiter.ArbiterHandlerChain, HandlerCapabilitySpec{
		Owner:       CapabilityOwnerCore,
		Description: "Route outcomes into another declared arbiter",
	})
	addHandler(sinkMap, arbiter.ArbiterHandlerWorker, HandlerCapabilitySpec{
		Owner:       CapabilityOwnerCore,
		Description: "Invoke a declared worker capability",
	})
	addHandler(sinkMap, arbiter.ArbiterHandlerAudit, HandlerCapabilitySpec{
		Owner:       CapabilityOwnerCore,
		Description: "Append deliveries to an audit sink",
	})
	addHandler(sinkMap, arbiter.ArbiterHandlerStdout, HandlerCapabilitySpec{
		Owner:       CapabilityOwnerCore,
		Description: "Write deliveries to stdout",
	})
	for kind := range opts.Handlers {
		addHandler(sinkMap, kind, opts.SinkCapabilities[kind])
	}
	for kind := range opts.WorkerHandlers {
		addHandler(workerMap, kind, opts.WorkerCapabilities[kind])
	}

	return CapabilitySurface{
		Sources: sortedSourceCapabilities(sourceMap),
		Sinks:   sortedHandlerCapabilities(sinkMap),
		Workers: sortedHandlerCapabilities(workerMap),
	}
}

func cloneCapabilitySurface(in CapabilitySurface) CapabilitySurface {
	return CapabilitySurface{
		Sources: slices.Clone(in.Sources),
		Sinks:   slices.Clone(in.Sinks),
		Workers: slices.Clone(in.Workers),
	}
}

func sortedSourceCapabilities(items map[string]SourceCapability) []SourceCapability {
	out := make([]SourceCapability, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	slices.SortFunc(out, func(a, b SourceCapability) int {
		return strings.Compare(a.Scheme, b.Scheme)
	})
	return out
}

func sortedHandlerCapabilities(items map[string]HandlerCapability) []HandlerCapability {
	out := make([]HandlerCapability, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	slices.SortFunc(out, func(a, b HandlerCapability) int {
		return strings.Compare(a.Kind, b.Kind)
	})
	return out
}

func normalizeCapabilityKey(value string) string {
	return strings.TrimSpace(value)
}

func normalizeCapabilityOwner(value CapabilityOwner, fallback CapabilityOwner) CapabilityOwner {
	switch value {
	case CapabilityOwnerCore, CapabilityOwnerHost, CapabilityOwnerPlugin:
		return value
	default:
		return fallback
	}
}
