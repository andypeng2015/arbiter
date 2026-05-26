package capability

import (
	"context"
	"net"
	"testing"

	arbiter "m31labs.dev/arbiter"
	arbiterv1 "m31labs.dev/arbiter/api/arbiter/v1"
	"m31labs.dev/arbiter/workflow"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestGRPCAdapterBindsRemoteSourceAndSinkCapabilities(t *testing.T) {
	src := []byte(`
fact Signal {
	level: string
}

outcome Alert {
	key: string
}

arbiter monitor {
	poll 1s
	source acme://feed/signals
	on Alert discord "room://ops"
}

expert rule EmitAlert priority 10 per_fact {
	when {
		any signal in facts.Signal { signal.level == "critical" }
	}
	then emit Alert {
		key: signal.key,
	}
}
`)

	w, err := workflow.Compile(src, workflow.Options{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	service := &testCapabilityService{}
	service.sources = []*arbiterv1.SourceCapability{{
		Scheme:      "acme",
		Description: "acme feed",
	}}
	service.sinks = []*arbiterv1.SinkCapability{{
		Kind:        "discord",
		Description: "discord sink",
	}}
	service.loadSource = func(_ context.Context, req *arbiterv1.LoadSourceRequest) (*arbiterv1.LoadSourceResponse, error) {
		if req.GetTarget() != "acme://feed/signals" {
			t.Fatalf("unexpected source target %q", req.GetTarget())
		}
		return &arbiterv1.LoadSourceResponse{
			Facts: []*arbiterv1.ExpertFact{{
				Type: "Signal",
				Key:  "sig-1",
				Fields: mustStruct(t, map[string]any{
					"level": "critical",
				}),
			}},
		}, nil
	}
	service.deliverOutcome = func(_ context.Context, req *arbiterv1.DeliverOutcomeRequest) (*arbiterv1.DeliverOutcomeResponse, error) {
		got := req.GetDelivery()
		if got.GetHandlerKind() != "discord" || got.GetHandlerTarget() != "room://ops" {
			t.Fatalf("unexpected delivery handler %+v", got)
		}
		if got.GetOutcome().GetName() != "Alert" || got.GetOutcome().GetParams().AsMap()["key"] != "sig-1" {
			t.Fatalf("unexpected delivery outcome %+v", got.GetOutcome())
		}
		service.deliveries++
		return &arbiterv1.DeliverOutcomeResponse{}, nil
	}

	client, cleanup := newCapabilityClient(t, service)
	defer cleanup()

	adapter := NewGRPCAdapter(client)
	opts, manifest, err := adapter.BindRunnerOptions(context.Background(), workflow.RunnerOptions{})
	if err != nil {
		t.Fatalf("BindRunnerOptions: %v", err)
	}
	if _, ok := manifest.Sinks[arbiter.ArbiterHandlerKind("discord")]; !ok {
		t.Fatalf("manifest missing discord sink: %+v", manifest)
	}

	runner, err := workflow.NewRunner(w, opts)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if !hasSourceCapability(runner.Capabilities().Sources, "acme", workflow.CapabilityOwnerPlugin) {
		t.Fatalf("runner capabilities missing plugin source: %+v", runner.Capabilities().Sources)
	}
	if !hasHandlerCapability(runner.Capabilities().Sinks, "discord", workflow.CapabilityOwnerPlugin) {
		t.Fatalf("runner capabilities missing plugin sink: %+v", runner.Capabilities().Sinks)
	}

	tick, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if service.deliveries != 1 {
		t.Fatalf("deliveries = %d, want 1", service.deliveries)
	}
	sink := tick.Sinks[`discord`+"\x00"+`room://ops`]
	if sink.Kind != "discord" || sink.Target != "room://ops" || !sink.Available {
		t.Fatalf("unexpected sink snapshot: %+v", sink)
	}
}

func TestGRPCAdapterBindsRemoteWorkerCapabilities(t *testing.T) {
	src := []byte(`
fact Lead {
	score: number
}

fact WorkerReceipt {
	status: string
}

outcome Qualified {
	key: string
}

worker notify_sales {
	input Qualified
	output WorkerReceipt
	python "sdk://notify-sales"
}

arbiter sales {
	poll 1s
	source acme://feed/leads
	source worker://notify_sales
	on Qualified worker notify_sales
}

expert rule QualifyLead priority 10 per_fact {
	when {
		any lead in facts.Lead { lead.score >= 90 }
	}
	then emit Qualified {
		key: lead.key,
	}
}
`)

	w, err := workflow.Compile(src, workflow.Options{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	service := &testCapabilityService{}
	service.sources = []*arbiterv1.SourceCapability{{Scheme: "acme"}}
	service.workers = []*arbiterv1.WorkerCapability{{
		Kind:        "python",
		Description: "python worker runtime",
	}}
	service.loadSource = func(_ context.Context, req *arbiterv1.LoadSourceRequest) (*arbiterv1.LoadSourceResponse, error) {
		if req.GetTarget() != "acme://feed/leads" {
			t.Fatalf("unexpected source target %q", req.GetTarget())
		}
		return &arbiterv1.LoadSourceResponse{
			Facts: []*arbiterv1.ExpertFact{{
				Type: "Lead",
				Key:  "lead-1",
				Fields: mustStruct(t, map[string]any{
					"score": float64(95),
				}),
			}},
		}, nil
	}
	service.executeWorker = func(_ context.Context, req *arbiterv1.ExecuteWorkerRequest) (*arbiterv1.ExecuteWorkerResponse, error) {
		if req.GetWorker().GetKind() != "python" || req.GetWorker().GetTarget() != "sdk://notify-sales" {
			t.Fatalf("unexpected worker spec %+v", req.GetWorker())
		}
		if req.GetDelivery().GetOutcome().GetName() != "Qualified" {
			t.Fatalf("unexpected worker outcome %+v", req.GetDelivery().GetOutcome())
		}
		service.executions++
		return &arbiterv1.ExecuteWorkerResponse{
			Facts: []*arbiterv1.ExpertFact{{
				Type: "WorkerReceipt",
				Key:  "lead-1",
				Fields: mustStruct(t, map[string]any{
					"status": "sent",
				}),
			}},
		}, nil
	}

	client, cleanup := newCapabilityClient(t, service)
	defer cleanup()

	adapter := NewGRPCAdapter(client)
	opts, manifest, err := adapter.BindRunnerOptions(context.Background(), workflow.RunnerOptions{})
	if err != nil {
		t.Fatalf("BindRunnerOptions: %v", err)
	}
	if _, ok := manifest.Workers[arbiter.ArbiterHandlerKind("python")]; !ok {
		t.Fatalf("manifest missing python worker: %+v", manifest)
	}

	runner, err := workflow.NewRunner(w, opts)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if !hasHandlerCapability(runner.Capabilities().Workers, "python", workflow.CapabilityOwnerPlugin) {
		t.Fatalf("runner capabilities missing plugin worker: %+v", runner.Capabilities().Workers)
	}

	tick, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if service.executions != 1 {
		t.Fatalf("executions = %d, want 1", service.executions)
	}
	workerSource := tick.Sources["worker://notify_sales"]
	if !workerSource.Available || workerSource.FactCount != 1 {
		t.Fatalf("unexpected worker source snapshot: %+v", workerSource)
	}
}

type testCapabilityService struct {
	arbiterv1.UnimplementedCapabilityServiceServer

	sources []*arbiterv1.SourceCapability
	sinks   []*arbiterv1.SinkCapability
	workers []*arbiterv1.WorkerCapability

	loadSource     func(context.Context, *arbiterv1.LoadSourceRequest) (*arbiterv1.LoadSourceResponse, error)
	deliverOutcome func(context.Context, *arbiterv1.DeliverOutcomeRequest) (*arbiterv1.DeliverOutcomeResponse, error)
	executeWorker  func(context.Context, *arbiterv1.ExecuteWorkerRequest) (*arbiterv1.ExecuteWorkerResponse, error)

	deliveries int
	executions int
}

func (s *testCapabilityService) GetCapabilities(context.Context, *arbiterv1.GetCapabilitiesRequest) (*arbiterv1.GetCapabilitiesResponse, error) {
	return &arbiterv1.GetCapabilitiesResponse{
		Name:    "test-plugin",
		Version: "dev",
		Sources: s.sources,
		Sinks:   s.sinks,
		Workers: s.workers,
	}, nil
}

func (s *testCapabilityService) LoadSource(ctx context.Context, req *arbiterv1.LoadSourceRequest) (*arbiterv1.LoadSourceResponse, error) {
	if s.loadSource == nil {
		return &arbiterv1.LoadSourceResponse{}, nil
	}
	return s.loadSource(ctx, req)
}

func (s *testCapabilityService) DeliverOutcome(ctx context.Context, req *arbiterv1.DeliverOutcomeRequest) (*arbiterv1.DeliverOutcomeResponse, error) {
	if s.deliverOutcome == nil {
		return &arbiterv1.DeliverOutcomeResponse{}, nil
	}
	return s.deliverOutcome(ctx, req)
}

func (s *testCapabilityService) ExecuteWorker(ctx context.Context, req *arbiterv1.ExecuteWorkerRequest) (*arbiterv1.ExecuteWorkerResponse, error) {
	if s.executeWorker == nil {
		return &arbiterv1.ExecuteWorkerResponse{}, nil
	}
	return s.executeWorker(ctx, req)
}

func newCapabilityClient(t *testing.T, srv arbiterv1.CapabilityServiceServer) (arbiterv1.CapabilityServiceClient, func()) {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	grpcSrv := grpc.NewServer()
	arbiterv1.RegisterCapabilityServiceServer(grpcSrv, srv)
	go func() {
		_ = grpcSrv.Serve(listener)
	}()

	dialer := func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		grpcSrv.Stop()
		t.Fatalf("grpc.NewClient: %v", err)
	}

	cleanup := func() {
		_ = conn.Close()
		grpcSrv.Stop()
		_ = listener.Close()
	}
	return arbiterv1.NewCapabilityServiceClient(conn), cleanup
}

func hasSourceCapability(items []workflow.SourceCapability, scheme string, owner workflow.CapabilityOwner) bool {
	for _, item := range items {
		if item.Scheme == scheme && item.Owner == owner {
			return true
		}
	}
	return false
}

func hasHandlerCapability(items []workflow.HandlerCapability, kind string, owner workflow.CapabilityOwner) bool {
	for _, item := range items {
		if item.Kind == kind && item.Owner == owner {
			return true
		}
	}
	return false
}

func mustStruct(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}
	return s
}
