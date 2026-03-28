// Command arbiter-runtime is the reference host process for continuous arbiters
// and workers. It loads a .arb file, compiles the workflow, and runs the arbiter
// loop with source polling, worker dispatch, delivery retry, and health endpoints.
//
// Usage:
//
//	arbiter-runtime --bundle rules.arb [--grpc 127.0.0.1:7081] [--capability-grpc 127.0.0.1:7090] [--poll 5s] [--status :7082] [--checkpoint /var/lib/arbiter/state] [--source-parallelism 8] [--delivery-parallelism 8]
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	goruntime "runtime"
	"sync"
	"syscall"
	"time"

	arbiter "github.com/odvcencio/arbiter"
	arbiterv1 "github.com/odvcencio/arbiter/api/arbiter/v1"
	"github.com/odvcencio/arbiter/capability"
	"github.com/odvcencio/arbiter/observability"
	"github.com/odvcencio/arbiter/workflow"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	bundlePath := flag.String("bundle", "", "path to .arb file")
	pollInterval := flag.Duration("poll", 5*time.Second, "tick interval")
	grpcAddr := flag.String("grpc", "", "optional runtime gRPC address for runtime control/introspection")
	statusAddr := flag.String("status", ":7082", "health/status HTTP address")
	checkpointDir := flag.String("checkpoint", "", "directory for checkpoint files")
	deliveryLog := flag.String("delivery-log", "", "path for delivery journal (JSONL)")
	capabilityGRPC := flag.String("capability-grpc", "", "optional gRPC capability service address for custom source/sink/worker runtimes")
	sourceParallelism := flag.Int("source-parallelism", defaultRuntimeParallelism(), "max concurrent external source loads per tick")
	deliveryParallelism := flag.Int("delivery-parallelism", defaultRuntimeParallelism(), "max concurrent handler delivery pipelines per tick")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
	flag.Parse()

	if *bundlePath == "" {
		fmt.Fprintln(os.Stderr, "Usage: arbiter-runtime --bundle <file.arb> [flags]")
		os.Exit(2)
	}

	logger := observability.NewLogger(observability.ParseLevel(*logLevel))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	rt, err := newRuntime(*bundlePath, runtimeConfig{
		pollInterval:        *pollInterval,
		grpcAddr:            *grpcAddr,
		statusAddr:          *statusAddr,
		checkpointDir:       *checkpointDir,
		deliveryLog:         *deliveryLog,
		capabilityGRPC:      *capabilityGRPC,
		sourceParallelism:   *sourceParallelism,
		deliveryParallelism: *deliveryParallelism,
	}, logger)
	if err != nil {
		logger.Error("init failed", observability.KeyError, err.Error())
		os.Exit(1)
	}

	if err := rt.run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("runtime error", observability.KeyError, err.Error())
		os.Exit(1)
	}
	logger.Info("shutdown complete")
}

type runtimeConfig struct {
	pollInterval        time.Duration
	grpcAddr            string
	statusAddr          string
	checkpointDir       string
	deliveryLog         string
	capabilityGRPC      string
	sourceParallelism   int
	deliveryParallelism int
}

func defaultRuntimeParallelism() int {
	parallelism := goruntime.GOMAXPROCS(0)
	if parallelism <= 0 {
		return 8
	}
	if parallelism > 8 {
		return 8
	}
	return parallelism
}

const runtimeTracerName = "arbiter.runtime"

type runtime struct {
	config runtimeConfig
	runner *workflow.Runner
	wf     *workflow.Workflow
	full   *arbiter.CompileResult
	logger *slog.Logger
	name   string
	conn   *grpc.ClientConn
	caps   *capability.Manifest

	mu         sync.RWMutex
	lastTick   time.Time
	lastResult workflow.TickResult
	tickCount  uint64
	errors     uint64
	ready      bool
}

func newRuntime(bundlePath string, config runtimeConfig, logger *slog.Logger) (*runtime, error) {
	if logger == nil {
		logger = observability.NewLogger(observability.ParseLevel("info"))
	}
	if config.sourceParallelism <= 0 {
		config.sourceParallelism = 1
	}
	if config.deliveryParallelism <= 0 {
		config.deliveryParallelism = 1
	}
	full, err := arbiter.CompileFullFile(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", bundlePath, err)
	}
	if len(full.Arbiters) == 0 {
		return nil, fmt.Errorf("%s: no arbiter declarations found", bundlePath)
	}

	wf, err := workflow.CompileFile(bundlePath, workflow.Options{})
	if err != nil {
		return nil, fmt.Errorf("compile workflow %s: %w", bundlePath, err)
	}

	runnerOpts := workflow.RunnerOptions{
		Handlers:                 defaultOutcomeHandlers(logger),
		SinkCapabilities:         defaultOutcomeCapabilitySpecs(),
		WorkerHandlers:           defaultWorkerHandlers(),
		WorkerCapabilities:       defaultWorkerCapabilitySpecs(),
		DeliveryLog:              config.deliveryLog,
		MaxConcurrentSourceLoads: config.sourceParallelism,
		MaxConcurrentDeliveries:  config.deliveryParallelism,
		Stdout:                   os.Stdout,
	}

	var capabilityConn *grpc.ClientConn
	var capabilityManifest *capability.Manifest
	if config.capabilityGRPC != "" {
		adapter, conn, manifest, err := dialCapabilityRuntime(config.capabilityGRPC)
		if err != nil {
			return nil, fmt.Errorf("connect capability service: %w", err)
		}
		runnerOpts, _, err = adapter.BindRunnerOptions(context.Background(), runnerOpts)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("bind capability service: %w", err)
		}
		capabilityConn = conn
		capabilityManifest = manifest
		logger.Info("capability service connected",
			"addr", config.capabilityGRPC,
			"plugin", manifest.Name,
			"version", manifest.Version,
			"source_schemes", len(manifest.Sources),
			"sink_kinds", len(manifest.Sinks),
			"worker_kinds", len(manifest.Workers))
	}

	runner, err := workflow.NewRunner(wf, runnerOpts)
	if err != nil {
		if capabilityConn != nil {
			_ = capabilityConn.Close()
		}
		return nil, fmt.Errorf("create runner: %w", err)
	}

	logger.Info("bundle loaded",
		observability.KeySource, bundlePath,
		observability.KeyArbiter, len(full.Arbiters),
		observability.KeyWorker, len(full.Workers),
		"external_sources", len(wf.ExternalSources()),
		"source_parallelism", config.sourceParallelism,
		"delivery_parallelism", config.deliveryParallelism)

	return &runtime{
		config: config,
		runner: runner,
		wf:     wf,
		full:   full,
		logger: logger,
		name:   bundlePath,
		conn:   capabilityConn,
		caps:   capabilityManifest,
	}, nil
}

func (rt *runtime) run(ctx context.Context) error {
	var grpcSrv *grpc.Server
	if rt.config.grpcAddr != "" {
		lis, err := net.Listen("tcp", rt.config.grpcAddr)
		if err != nil {
			return fmt.Errorf("listen %s: %w", rt.config.grpcAddr, err)
		}
		grpcSrv = grpc.NewServer()
		arbiterv1.RegisterRuntimeServiceServer(grpcSrv, newRuntimeRPCServer(rt))
		go func() {
			rt.logger.Info("runtime gRPC listening", "addr", rt.config.grpcAddr)
			if err := grpcSrv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
				rt.logger.Error("runtime gRPC server error", observability.KeyError, err.Error())
			}
		}()
	}

	// Start health server.
	srv := &http.Server{Addr: rt.config.statusAddr, Handler: rt.statusMux()}
	go func() {
		rt.logger.Info("status server listening", "addr", rt.config.statusAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			rt.logger.Error("status server error", observability.KeyError, err.Error())
		}
	}()

	ticker := time.NewTicker(rt.config.pollInterval)
	defer ticker.Stop()

	// First tick immediately.
	rt.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if grpcSrv != nil {
				grpcSrv.GracefulStop()
			}
			_ = srv.Shutdown(shutdownCtx)
			drained, retried, err := rt.runner.Drain(shutdownCtx)
			switch {
			case err != nil && shutdownCtx.Err() != nil:
				rt.logger.Warn("shutdown drain timed out",
					"delivered", drained,
					"retried", retried,
					observability.KeyError, err.Error())
			case err != nil:
				rt.logger.Error("shutdown drain error", observability.KeyError, err.Error())
			default:
				rt.logger.Info("shutdown drain complete",
					"delivered", drained,
					"retried", retried)
			}
			_ = rt.runner.Close()
			if rt.conn != nil {
				_ = rt.conn.Close()
			}
			return nil
		case <-ticker.C:
			rt.tick(ctx)
		}
	}
}

func dialCapabilityRuntime(target string) (*capability.GRPCAdapter, *grpc.ClientConn, *capability.Manifest, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, nil, err
	}
	adapter := capability.NewGRPCAdapter(arbiterv1.NewCapabilityServiceClient(conn))
	manifest, err := adapter.Discover(context.Background())
	if err != nil {
		_ = conn.Close()
		return nil, nil, nil, err
	}
	return adapter, conn, manifest, nil
}

func (rt *runtime) tick(ctx context.Context) {
	ctx, span := otel.Tracer(runtimeTracerName).Start(ctx, "arbiter.runtime.tick")
	span.SetAttributes(attribute.String("arbiter.arbiter_name", rt.name))
	defer span.End()

	start := time.Now()
	result, err := rt.runner.Tick(ctx)

	rt.mu.Lock()
	rt.lastTick = start
	rt.tickCount++
	rt.ready = true
	if err != nil {
		rt.errors++
		rt.logger.Error("tick error",
			"tick", rt.tickCount,
			observability.KeyError, err.Error())
	} else {
		rt.lastResult = result
	}
	tick := rt.tickCount
	rt.mu.Unlock()

	if err == nil {
		outcomes := 0
		for _, arb := range result.Workflow.Arbiters {
			outcomes += len(arb.Delta.Outcomes)
		}
		if outcomes > 0 || result.Delivered > 0 {
			rt.logger.Info("tick complete",
				"tick", tick,
				"outcomes", outcomes,
				"delivered", result.Delivered,
				"enqueued", result.Enqueued,
				"retried", result.Retried,
				"duration_ms", int64(time.Since(start).Seconds()*1000))
		}
	}
}

// --- Health Endpoints ---

func (rt *runtime) statusMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", rt.handleHealthz)
	mux.HandleFunc("/readyz", rt.handleReadyz)
	mux.HandleFunc("/status", rt.handleStatus)
	return mux
}

func (rt *runtime) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (rt *runtime) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	rt.mu.RLock()
	ready := rt.ready
	rt.mu.RUnlock()
	if !ready {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not ready"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (rt *runtime) handleStatus(w http.ResponseWriter, _ *http.Request) {
	rt.mu.RLock()
	status := map[string]any{
		"ready":              rt.ready,
		"ticks":              rt.tickCount,
		"errors":             rt.errors,
		"last_tick":          rt.lastTick,
		"sources":            rt.lastResult.Sources,
		"sinks":              rt.lastResult.Sinks,
		"delivered":          rt.lastResult.Delivered,
		"enqueued":           rt.lastResult.Enqueued,
		"retried":            rt.lastResult.Retried,
		"capabilities":       capabilityStatus(rt.runner.Capabilities()),
		"capability_plugins": capabilityPluginsStatus(rt.caps),
	}
	rt.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func capabilityStatus(surface workflow.CapabilitySurface) map[string]any {
	sources := make([]map[string]any, 0, len(surface.Sources))
	for _, item := range surface.Sources {
		sources = append(sources, map[string]any{
			"scheme":      item.Scheme,
			"owner":       string(item.Owner),
			"description": item.Description,
		})
	}

	sinks := make([]map[string]any, 0, len(surface.Sinks))
	for _, item := range surface.Sinks {
		sinks = append(sinks, map[string]any{
			"kind":        item.Kind,
			"owner":       string(item.Owner),
			"description": item.Description,
		})
	}

	workers := make([]map[string]any, 0, len(surface.Workers))
	for _, item := range surface.Workers {
		workers = append(workers, map[string]any{
			"kind":        item.Kind,
			"owner":       string(item.Owner),
			"description": item.Description,
		})
	}

	return map[string]any{
		"sources": sources,
		"sinks":   sinks,
		"workers": workers,
	}
}

func capabilityPluginsStatus(manifest *capability.Manifest) []map[string]any {
	if manifest == nil {
		return nil
	}
	return []map[string]any{{
		"name":    manifest.Name,
		"version": manifest.Version,
	}}
}

// --- Default Handlers ---

func defaultOutcomeHandlers(logger *slog.Logger) map[arbiter.ArbiterHandlerKind]workflow.OutcomeHandler {
	logHandler := workflow.OutcomeHandlerFunc(func(_ context.Context, d workflow.Delivery) error {
		logger.Info("outcome delivered",
			observability.KeyHandlerKind, string(d.Handler.Kind),
			observability.KeyArbiter, d.Arbiter,
			"outcome", d.Outcome.Name,
			"target", d.Handler.Target)
		return nil
	})
	return map[arbiter.ArbiterHandlerKind]workflow.OutcomeHandler{
		arbiter.ArbiterHandlerWebhook: workflow.OutcomeHandlerFunc(deliverWebhook),
		arbiter.ArbiterHandlerExec:    workflow.OutcomeHandlerFunc(deliverExec),
		arbiter.ArbiterHandlerSlack:   logHandler,
		arbiter.ArbiterHandlerGRPC:    logHandler,
	}
}

func defaultOutcomeCapabilitySpecs() map[arbiter.ArbiterHandlerKind]workflow.HandlerCapabilitySpec {
	return map[arbiter.ArbiterHandlerKind]workflow.HandlerCapabilitySpec{
		arbiter.ArbiterHandlerWebhook: {
			Owner:       workflow.CapabilityOwnerHost,
			Description: "POST deliveries to an HTTP endpoint",
		},
		arbiter.ArbiterHandlerExec: {
			Owner:       workflow.CapabilityOwnerHost,
			Description: "Run a local command for each delivery",
		},
		arbiter.ArbiterHandlerSlack: {
			Owner:       workflow.CapabilityOwnerHost,
			Description: "Reference-runtime sink placeholder that logs deliveries",
		},
		arbiter.ArbiterHandlerGRPC: {
			Owner:       workflow.CapabilityOwnerHost,
			Description: "Reference-runtime sink placeholder that logs deliveries",
		},
	}
}

func defaultWorkerHandlers() map[arbiter.ArbiterHandlerKind]workflow.WorkerHandler {
	return map[arbiter.ArbiterHandlerKind]workflow.WorkerHandler{
		arbiter.ArbiterHandlerExec:    workflow.WorkerHandlerFunc(executeWorkerExec),
		arbiter.ArbiterHandlerWebhook: workflow.WorkerHandlerFunc(executeWorkerWebhook),
	}
}

func defaultWorkerCapabilitySpecs() map[arbiter.ArbiterHandlerKind]workflow.HandlerCapabilitySpec {
	return map[arbiter.ArbiterHandlerKind]workflow.HandlerCapabilitySpec{
		arbiter.ArbiterHandlerExec: {
			Owner:       workflow.CapabilityOwnerHost,
			Description: "Run a local command and decode structured worker output",
		},
		arbiter.ArbiterHandlerWebhook: {
			Owner:       workflow.CapabilityOwnerHost,
			Description: "POST a worker invocation and decode the response payload",
		},
	}
}

// --- Webhook Handler ---

func deliverWebhook(ctx context.Context, d workflow.Delivery) error {
	payload, err := json.Marshal(d)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", d.Handler.Target, jsonReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook %s: HTTP %d", d.Handler.Target, resp.StatusCode)
	}
	return nil
}

// --- Exec Handler ---

func deliverExec(ctx context.Context, d workflow.Delivery) error {
	payload, _ := json.Marshal(d)
	return runExecCommand(ctx, d.Handler.Target, payload)
}

func executeWorkerExec(ctx context.Context, inv workflow.WorkerInvocation) (workflow.WorkerExecution, error) {
	ctx, span := otel.Tracer(runtimeTracerName).Start(ctx, "arbiter.worker.dispatch")
	span.SetAttributes(
		attribute.String("arbiter.worker_name", inv.Worker.Name),
		attribute.String("arbiter.handler_kind", string(inv.Worker.Kind)),
	)
	defer span.End()

	payload, _ := json.Marshal(inv.Delivery)
	output, err := runExecCommandOutput(ctx, inv.Worker.Target, payload)
	if err != nil {
		return workflow.WorkerExecution{}, err
	}
	var result workflow.WorkerExecution
	if err := json.Unmarshal(output, &result); err != nil {
		return workflow.WorkerExecution{}, fmt.Errorf("worker %s: invalid output: %w", inv.Worker.Name, err)
	}
	return result, nil
}

func executeWorkerWebhook(ctx context.Context, inv workflow.WorkerInvocation) (workflow.WorkerExecution, error) {
	ctx, span := otel.Tracer(runtimeTracerName).Start(ctx, "arbiter.worker.dispatch")
	span.SetAttributes(
		attribute.String("arbiter.worker_name", inv.Worker.Name),
		attribute.String("arbiter.handler_kind", string(inv.Worker.Kind)),
	)
	defer span.End()

	payload, _ := json.Marshal(inv.Delivery)
	req, err := http.NewRequestWithContext(ctx, "POST", inv.Worker.Target, jsonReader(payload))
	if err != nil {
		return workflow.WorkerExecution{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return workflow.WorkerExecution{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return workflow.WorkerExecution{}, fmt.Errorf("worker webhook %s: HTTP %d", inv.Worker.Target, resp.StatusCode)
	}
	var result workflow.WorkerExecution
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return workflow.WorkerExecution{}, fmt.Errorf("worker %s: invalid response: %w", inv.Worker.Name, err)
	}
	return result, nil
}

// --- Helpers ---

func jsonReader(data []byte) *bytes.Reader {
	return bytes.NewReader(data)
}
