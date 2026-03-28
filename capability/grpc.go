package capability

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	arbiter "github.com/odvcencio/arbiter"
	arbiterv1 "github.com/odvcencio/arbiter/api/arbiter/v1"
	"github.com/odvcencio/arbiter/expert"
	"github.com/odvcencio/arbiter/expert/factsource"
	"github.com/odvcencio/arbiter/workflow"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SourceRegistration describes one remote source scheme the capability service owns.
type SourceRegistration struct {
	Scheme      string
	Description string
}

// SinkRegistration describes one remote sink kind the capability service owns.
type SinkRegistration struct {
	Kind        arbiter.ArbiterHandlerKind
	Description string
}

// WorkerRegistration describes one remote worker runtime kind the capability service owns.
type WorkerRegistration struct {
	Kind        arbiter.ArbiterHandlerKind
	Description string
}

// Manifest is one remote capability service's declared runtime surface.
type Manifest struct {
	Name    string
	Version string
	Sources map[string]SourceRegistration
	Sinks   map[arbiter.ArbiterHandlerKind]SinkRegistration
	Workers map[arbiter.ArbiterHandlerKind]WorkerRegistration
}

// GRPCAdapter binds one remote capability service into workflow runner hooks.
type GRPCAdapter struct {
	client   arbiterv1.CapabilityServiceClient
	mu       sync.RWMutex
	manifest *Manifest
}

// NewGRPCAdapter creates a workflow capability adapter backed by gRPC.
func NewGRPCAdapter(client arbiterv1.CapabilityServiceClient) *GRPCAdapter {
	return &GRPCAdapter{client: client}
}

// Discover fetches and caches the remote capability manifest.
func (a *GRPCAdapter) Discover(ctx context.Context) (*Manifest, error) {
	if a == nil || a.client == nil {
		return nil, errors.New("capability client is required")
	}

	a.mu.RLock()
	if a.manifest != nil {
		manifest := cloneManifest(a.manifest)
		a.mu.RUnlock()
		return manifest, nil
	}
	a.mu.RUnlock()

	resp, err := a.client.GetCapabilities(ctx, &arbiterv1.GetCapabilitiesRequest{})
	if err != nil {
		return nil, err
	}
	manifest := manifestFromProto(resp)

	a.mu.Lock()
	a.manifest = cloneManifest(manifest)
	a.mu.Unlock()

	return manifest, nil
}

// BindRunnerOptions discovers remote capabilities and returns runner options wired to call them.
func (a *GRPCAdapter) BindRunnerOptions(ctx context.Context, opts workflow.RunnerOptions) (workflow.RunnerOptions, *Manifest, error) {
	manifest, err := a.Discover(ctx)
	if err != nil {
		return opts, nil, err
	}
	if len(manifest.Sources) > 0 && opts.SourceCapabilities == nil {
		opts.SourceCapabilities = make(map[string]workflow.SourceCapabilitySpec, len(manifest.Sources))
	}
	for scheme, item := range manifest.Sources {
		opts.SourceCapabilities[scheme] = workflow.SourceCapabilitySpec{
			Owner:       workflow.CapabilityOwnerPlugin,
			Description: item.Description,
		}
	}

	baseLoader := opts.Loader
	if baseLoader == nil {
		baseLoader = defaultSourceLoader
	}
	opts.Loader = func(ctx context.Context, target string) ([]expert.Fact, error) {
		scheme := sourceScheme(target)
		if _, ok := manifest.Sources[scheme]; ok {
			resp, err := a.client.LoadSource(ctx, &arbiterv1.LoadSourceRequest{Target: target})
			if err != nil {
				return nil, err
			}
			return expertFactsFromProto(resp.GetFacts())
		}
		return baseLoader(ctx, target)
	}

	if len(manifest.Sinks) > 0 && opts.Handlers == nil {
		opts.Handlers = make(map[arbiter.ArbiterHandlerKind]workflow.OutcomeHandler, len(manifest.Sinks))
	}
	if len(manifest.Sinks) > 0 && opts.SinkCapabilities == nil {
		opts.SinkCapabilities = make(map[arbiter.ArbiterHandlerKind]workflow.HandlerCapabilitySpec, len(manifest.Sinks))
	}
	for kind := range manifest.Sinks {
		if _, exists := opts.Handlers[kind]; exists {
			return opts, nil, fmt.Errorf("capability sink kind %q already registered", kind)
		}
		opts.SinkCapabilities[kind] = workflow.HandlerCapabilitySpec{
			Owner:       workflow.CapabilityOwnerPlugin,
			Description: manifest.Sinks[kind].Description,
		}
		opts.Handlers[kind] = workflow.OutcomeHandlerFunc(func(ctx context.Context, delivery workflow.Delivery) error {
			req, err := protoDeliverOutcomeRequest(delivery)
			if err != nil {
				return err
			}
			_, err = a.client.DeliverOutcome(ctx, req)
			return err
		})
	}

	if len(manifest.Workers) > 0 && opts.WorkerHandlers == nil {
		opts.WorkerHandlers = make(map[arbiter.ArbiterHandlerKind]workflow.WorkerHandler, len(manifest.Workers))
	}
	if len(manifest.Workers) > 0 && opts.WorkerCapabilities == nil {
		opts.WorkerCapabilities = make(map[arbiter.ArbiterHandlerKind]workflow.HandlerCapabilitySpec, len(manifest.Workers))
	}
	for kind := range manifest.Workers {
		if _, exists := opts.WorkerHandlers[kind]; exists {
			return opts, nil, fmt.Errorf("capability worker kind %q already registered", kind)
		}
		opts.WorkerCapabilities[kind] = workflow.HandlerCapabilitySpec{
			Owner:       workflow.CapabilityOwnerPlugin,
			Description: manifest.Workers[kind].Description,
		}
		opts.WorkerHandlers[kind] = workflow.WorkerHandlerFunc(func(ctx context.Context, invocation workflow.WorkerInvocation) (workflow.WorkerExecution, error) {
			req, err := protoExecuteWorkerRequest(invocation)
			if err != nil {
				return workflow.WorkerExecution{}, err
			}
			resp, err := a.client.ExecuteWorker(ctx, req)
			if err != nil {
				return workflow.WorkerExecution{}, err
			}
			facts, err := expertFactsFromProto(resp.GetFacts())
			if err != nil {
				return workflow.WorkerExecution{}, err
			}
			outcomes, err := expertOutcomesFromProto(resp.GetOutcomes())
			if err != nil {
				return workflow.WorkerExecution{}, err
			}
			return workflow.WorkerExecution{Facts: facts, Outcomes: outcomes}, nil
		})
	}

	return opts, manifest, nil
}

func manifestFromProto(resp *arbiterv1.GetCapabilitiesResponse) *Manifest {
	manifest := &Manifest{
		Name:    strings.TrimSpace(resp.GetName()),
		Version: strings.TrimSpace(resp.GetVersion()),
		Sources: make(map[string]SourceRegistration, len(resp.GetSources())),
		Sinks:   make(map[arbiter.ArbiterHandlerKind]SinkRegistration, len(resp.GetSinks())),
		Workers: make(map[arbiter.ArbiterHandlerKind]WorkerRegistration, len(resp.GetWorkers())),
	}
	for _, item := range resp.GetSources() {
		scheme := strings.ToLower(strings.TrimSpace(item.GetScheme()))
		if scheme == "" {
			continue
		}
		manifest.Sources[scheme] = SourceRegistration{
			Scheme:      scheme,
			Description: strings.TrimSpace(item.GetDescription()),
		}
	}
	for _, item := range resp.GetSinks() {
		kind := arbiter.ArbiterHandlerKind(strings.TrimSpace(item.GetKind()))
		if kind == "" {
			continue
		}
		manifest.Sinks[kind] = SinkRegistration{
			Kind:        kind,
			Description: strings.TrimSpace(item.GetDescription()),
		}
	}
	for _, item := range resp.GetWorkers() {
		kind := arbiter.ArbiterHandlerKind(strings.TrimSpace(item.GetKind()))
		if kind == "" {
			continue
		}
		manifest.Workers[kind] = WorkerRegistration{
			Kind:        kind,
			Description: strings.TrimSpace(item.GetDescription()),
		}
	}
	return manifest
}

func cloneManifest(in *Manifest) *Manifest {
	if in == nil {
		return nil
	}
	out := &Manifest{
		Name:    in.Name,
		Version: in.Version,
		Sources: make(map[string]SourceRegistration, len(in.Sources)),
		Sinks:   make(map[arbiter.ArbiterHandlerKind]SinkRegistration, len(in.Sinks)),
		Workers: make(map[arbiter.ArbiterHandlerKind]WorkerRegistration, len(in.Workers)),
	}
	for k, v := range in.Sources {
		out.Sources[k] = v
	}
	for k, v := range in.Sinks {
		out.Sinks[k] = v
	}
	for k, v := range in.Workers {
		out.Workers[k] = v
	}
	return out
}

func sourceScheme(target string) string {
	i := strings.Index(target, "://")
	if i <= 0 {
		return ""
	}
	return strings.ToLower(target[:i])
}

func defaultSourceLoader(_ context.Context, target string) ([]expert.Fact, error) {
	facts, err := factsource.Load(target)
	if err != nil {
		return nil, err
	}
	out := make([]expert.Fact, 0, len(facts))
	for _, fact := range facts {
		out = append(out, expert.Fact{
			Type:   fact.Type,
			Key:    fact.Key,
			Fields: cloneMap(fact.Fields),
		})
	}
	return out, nil
}

func protoDeliverOutcomeRequest(delivery workflow.Delivery) (*arbiterv1.DeliverOutcomeRequest, error) {
	outcome, err := protoOutcome(delivery.Outcome)
	if err != nil {
		return nil, err
	}
	return &arbiterv1.DeliverOutcomeRequest{
		Delivery: &arbiterv1.DeliveryContext{
			DeliveryId:    delivery.ID,
			ArbiterName:   delivery.Arbiter,
			WorkerName:    delivery.Worker,
			HandlerKind:   string(delivery.Handler.Kind),
			HandlerTarget: delivery.Handler.Target,
			Outcome:       outcome,
			Attempt:       uint32(delivery.Attempt),
			EnqueuedAt:    timestamppb.New(delivery.EnqueuedAt),
			LastAttemptAt: timestamppb.New(delivery.LastAttemptAt),
			NextAttemptAt: timestamppb.New(delivery.NextAttemptAt),
			LastError:     delivery.LastError,
		},
	}, nil
}

func protoExecuteWorkerRequest(invocation workflow.WorkerInvocation) (*arbiterv1.ExecuteWorkerRequest, error) {
	delivery, err := protoDeliverOutcomeRequest(invocation.Delivery)
	if err != nil {
		return nil, err
	}
	return &arbiterv1.ExecuteWorkerRequest{
		Worker: &arbiterv1.WorkerSpec{
			Name:       invocation.Worker.Name,
			Input:      invocation.Worker.Input,
			Output:     invocation.Worker.Output,
			OutputKind: protoWorkerOutputKind(invocation.Worker.OutputKind),
			Kind:       string(invocation.Worker.Kind),
			Target:     invocation.Worker.Target,
		},
		Delivery: delivery.GetDelivery(),
	}, nil
}

func protoWorkerOutputKind(kind arbiter.WorkerOutputKind) arbiterv1.WorkerOutputKind {
	switch kind {
	case arbiter.WorkerOutputFact:
		return arbiterv1.WorkerOutputKind_WORKER_OUTPUT_KIND_FACT
	case arbiter.WorkerOutputOutcome:
		return arbiterv1.WorkerOutputKind_WORKER_OUTPUT_KIND_OUTCOME
	default:
		return arbiterv1.WorkerOutputKind_WORKER_OUTPUT_KIND_UNSPECIFIED
	}
}

func protoOutcome(item expert.Outcome) (*arbiterv1.ExpertOutcome, error) {
	params, err := structpb.NewStruct(cleanMap(item.Params))
	if err != nil {
		return nil, fmt.Errorf("marshal outcome params: %w", err)
	}
	return &arbiterv1.ExpertOutcome{
		Rule:   item.Rule,
		Name:   item.Name,
		Params: params,
	}, nil
}

func expertFactsFromProto(items []*arbiterv1.ExpertFact) ([]expert.Fact, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]expert.Fact, 0, len(items))
	for _, item := range items {
		if item.GetType() == "" {
			return nil, fmt.Errorf("fact type is required")
		}
		if item.GetKey() == "" {
			return nil, fmt.Errorf("fact key is required")
		}
		fields := map[string]any{}
		if item.Fields != nil {
			fields = item.Fields.AsMap()
		}
		out = append(out, expert.Fact{
			Type:   item.GetType(),
			Key:    item.GetKey(),
			Fields: fields,
		})
	}
	return out, nil
}

func expertOutcomesFromProto(items []*arbiterv1.ExpertOutcome) ([]expert.Outcome, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]expert.Outcome, 0, len(items))
	for _, item := range items {
		if item.GetName() == "" {
			return nil, fmt.Errorf("outcome name is required")
		}
		params := map[string]any{}
		if item.Params != nil {
			params = item.Params.AsMap()
		}
		out = append(out, expert.Outcome{
			Rule:   item.GetRule(),
			Name:   item.GetName(),
			Params: params,
		})
	}
	return out, nil
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch value := v.(type) {
	case nil:
		return nil
	case map[string]any:
		return cloneMap(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = cloneValue(item)
		}
		return out
	case []string:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	case []int:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	case []float64:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	case []bool:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	default:
		return value
	}
}

func cleanMap(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cleanValue(v)
	}
	return out
}

func cleanValue(v any) any {
	switch value := v.(type) {
	case nil:
		return nil
	case map[string]any:
		return cleanMap(value)
	case []any:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = cleanValue(item)
		}
		return out
	case []string:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	case []int:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	case []float64:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	case []bool:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = item
		}
		return out
	default:
		return value
	}
}
