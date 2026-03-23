// Command arbiter-runtime is the reference host process for continuous arbiters
// and workers. It loads a .arb file, compiles the workflow, and runs the arbiter
// loop with source polling, worker dispatch, delivery retry, and health endpoints.
//
// Usage:
//
//	arbiter-runtime --bundle rules.arb [--poll 5s] [--status :7082] [--checkpoint /var/lib/arbiter/state]
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/workflow"
)

func main() {
	bundlePath := flag.String("bundle", "", "path to .arb file")
	pollInterval := flag.Duration("poll", 5*time.Second, "tick interval")
	statusAddr := flag.String("status", ":7082", "health/status HTTP address")
	checkpointDir := flag.String("checkpoint", "", "directory for checkpoint files")
	deliveryLog := flag.String("delivery-log", "", "path for delivery journal (JSONL)")
	flag.Parse()

	if *bundlePath == "" {
		fmt.Fprintln(os.Stderr, "Usage: arbiter-runtime --bundle <file.arb> [flags]")
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	rt, err := newRuntime(*bundlePath, runtimeConfig{
		pollInterval:  *pollInterval,
		statusAddr:    *statusAddr,
		checkpointDir: *checkpointDir,
		deliveryLog:   *deliveryLog,
	})
	if err != nil {
		log.Fatalf("init: %v", err)
	}

	if err := rt.run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("runtime: %v", err)
	}
	log.Println("shutdown complete")
}

type runtimeConfig struct {
	pollInterval  time.Duration
	statusAddr    string
	checkpointDir string
	deliveryLog   string
}

type runtime struct {
	config  runtimeConfig
	runner  *workflow.Runner
	wf      *workflow.Workflow
	full    *arbiter.CompileResult

	mu         sync.RWMutex
	lastTick   time.Time
	lastResult workflow.TickResult
	tickCount  uint64
	errors     uint64
	ready      bool
}

func newRuntime(bundlePath string, config runtimeConfig) (*runtime, error) {
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

	runner, err := workflow.NewRunner(wf, workflow.RunnerOptions{
		Handlers:       defaultOutcomeHandlers(),
		WorkerHandlers: defaultWorkerHandlers(),
		DeliveryLog:    config.deliveryLog,
		Stdout:         os.Stdout,
	})
	if err != nil {
		return nil, fmt.Errorf("create runner: %w", err)
	}

	log.Printf("loaded %s: %d arbiters, %d workers, %d external sources",
		bundlePath, len(full.Arbiters), len(full.Workers), len(wf.ExternalSources()))

	return &runtime{
		config: config,
		runner: runner,
		wf:     wf,
		full:   full,
	}, nil
}

func (rt *runtime) run(ctx context.Context) error {
	// Start health server.
	srv := &http.Server{Addr: rt.config.statusAddr, Handler: rt.statusMux()}
	go func() {
		log.Printf("status server listening on %s", rt.config.statusAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("status server: %v", err)
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
			srv.Shutdown(shutdownCtx)
			rt.runner.Close()
			return nil
		case <-ticker.C:
			rt.tick(ctx)
		}
	}
}

func (rt *runtime) tick(ctx context.Context) {
	start := time.Now()
	result, err := rt.runner.Tick(ctx)

	rt.mu.Lock()
	rt.lastTick = start
	rt.tickCount++
	rt.ready = true
	if err != nil {
		rt.errors++
		log.Printf("tick %d: error: %v", rt.tickCount, err)
	} else {
		rt.lastResult = result
	}
	rt.mu.Unlock()

	if err == nil {
		outcomes := 0
		for _, arb := range result.Workflow.Arbiters {
			outcomes += len(arb.Delta.Outcomes)
		}
		if outcomes > 0 || result.Delivered > 0 {
			log.Printf("tick %d: %d outcomes, %d delivered, %d enqueued, %d retried (%.0fms)",
				rt.tickCount, outcomes, result.Delivered, result.Enqueued, result.Retried,
				time.Since(start).Seconds()*1000)
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
		"ready":     rt.ready,
		"ticks":     rt.tickCount,
		"errors":    rt.errors,
		"last_tick": rt.lastTick,
		"sources":   rt.lastResult.Sources,
		"sinks":     rt.lastResult.Sinks,
		"delivered": rt.lastResult.Delivered,
		"enqueued":  rt.lastResult.Enqueued,
		"retried":   rt.lastResult.Retried,
	}
	rt.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// --- Default Handlers ---

func defaultOutcomeHandlers() map[arbiter.ArbiterHandlerKind]workflow.OutcomeHandler {
	logHandler := workflow.OutcomeHandlerFunc(func(_ context.Context, d workflow.Delivery) error {
		log.Printf("[%s] %s → %s %s", d.Handler.Kind, d.Arbiter, d.Outcome.Name, d.Handler.Target)
		return nil
	})
	return map[arbiter.ArbiterHandlerKind]workflow.OutcomeHandler{
		arbiter.ArbiterHandlerWebhook: workflow.OutcomeHandlerFunc(deliverWebhook),
		arbiter.ArbiterHandlerExec:    workflow.OutcomeHandlerFunc(deliverExec),
		arbiter.ArbiterHandlerSlack:   logHandler,
		arbiter.ArbiterHandlerGRPC:    logHandler,
	}
}

func defaultWorkerHandlers() map[arbiter.ArbiterHandlerKind]workflow.WorkerHandler {
	return map[arbiter.ArbiterHandlerKind]workflow.WorkerHandler{
		arbiter.ArbiterHandlerExec:    workflow.WorkerHandlerFunc(executeWorkerExec),
		arbiter.ArbiterHandlerWebhook: workflow.WorkerHandlerFunc(executeWorkerWebhook),
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
