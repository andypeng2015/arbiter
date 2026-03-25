package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/expert"
)

func writeDeliveryJournal(t *testing.T, path string, entries ...deliveryJournalEntry) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	for _, entry := range entries {
		if err := enc.Encode(entry); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}
}

func TestRunnerKeepsLastKnownGoodFactsAndExposesSourceStaleness(t *testing.T) {
	src := []byte(`
arbiter sales {
	poll 1s
	source https://feed.internal/facts
}

expert rule QualifyLead priority 10 per_fact {
	when {
		any lead in facts.Lead { lead.score >= 90 }
	}
	then emit Qualified {
		key: lead.key,
	}
}

expert rule AlertOnStaleSource priority 5 {
	when {
		source.feed_internal_facts.available == false
		and source.feed_internal_facts.__source_age_seconds >= 60
	}
	then emit SourceStale {
		age_seconds: source.feed_internal_facts.__source_age_seconds,
	}
}
`)

	w, err := Compile(src, Options{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	attempts := 0
	now := time.Unix(1_000, 0).UTC()
	runner, err := NewRunner(w, RunnerOptions{
		Now: func() time.Time { return now },
		Loader: func(_ context.Context, target string) ([]expert.Fact, error) {
			attempts++
			if target != "https://feed.internal/facts" {
				t.Fatalf("unexpected target %q", target)
			}
			if attempts == 1 {
				return []expert.Fact{{
					Type: "Lead",
					Key:  "lead-1",
					Fields: map[string]any{
						"score": float64(95),
					},
				}}, nil
			}
			return nil, errors.New("source unavailable")
		},
		InitialBackoff: time.Millisecond,
		MaxBackoff:     2 * time.Millisecond,
		SourceAttempts: 2,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	first, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	if got := first.Workflow.Arbiters["sales"].Delta.Outcomes; len(got) != 1 || got[0].Name != "Qualified" {
		t.Fatalf("first outcomes = %+v", got)
	}

	now = now.Add(70 * time.Second)
	second, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if got := second.Workflow.Arbiters["sales"].Delta.Outcomes; len(got) != 1 || got[0].Name != "SourceStale" {
		t.Fatalf("second outcomes = %+v", got)
	}
	if got := second.Workflow.Arbiters["sales"].Delta.Outcomes[0].Params["age_seconds"]; got != float64(70) {
		t.Fatalf("age_seconds = %#v, want 70", got)
	}
	state := second.Sources["https://feed.internal/facts"]
	if state.Available {
		t.Fatalf("source state = %+v, want unavailable", state)
	}
	if state.ConsecutiveFailures != 1 {
		t.Fatalf("source failures = %d, want 1", state.ConsecutiveFailures)
	}
	if state.FactCount != 1 {
		t.Fatalf("source fact count = %d, want 1", state.FactCount)
	}

	third, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("third Tick: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts after backoff skip = %d, want 3", attempts)
	}
	if got := third.Workflow.Arbiters["sales"].Delta.Outcomes; len(got) != 0 {
		t.Fatalf("third outcomes = %+v, want none during backoff window", got)
	}
}

func TestRunnerTickKeepsSourceSnapshotsReadableDuringSlowLoad(t *testing.T) {
	src := []byte(`
arbiter sales {
	poll 1s
	source https://feed.internal/facts
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

	w, err := Compile(src, Options{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	loadStarted := make(chan struct{})
	releaseLoad := make(chan struct{})
	runner, err := NewRunner(w, RunnerOptions{
		Loader: func(_ context.Context, target string) ([]expert.Fact, error) {
			if target != "https://feed.internal/facts" {
				t.Fatalf("unexpected target %q", target)
			}
			close(loadStarted)
			<-releaseLoad
			return []expert.Fact{{
				Type: "Lead",
				Key:  "lead-1",
				Fields: map[string]any{
					"score": float64(95),
				},
			}}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	tickDone := make(chan error, 1)
	go func() {
		_, err := runner.Tick(context.Background())
		tickDone <- err
	}()

	select {
	case <-loadStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for source load to start")
	}

	snapshotDone := make(chan map[string]SourceSnapshot, 1)
	go func() {
		snapshotDone <- runner.SourceStates()
	}()

	select {
	case sources := <-snapshotDone:
		state, ok := sources["https://feed.internal/facts"]
		if !ok {
			t.Fatalf("source snapshot missing target: %+v", sources)
		}
		if state.Target != "https://feed.internal/facts" {
			t.Fatalf("unexpected source snapshot: %+v", state)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("SourceStates blocked during slow source load")
	}

	close(releaseLoad)
	if err := <-tickDone; err != nil {
		t.Fatalf("Tick: %v", err)
	}
}

func TestRunnerRestoresPendingSinkDeliveriesFromJournal(t *testing.T) {
	src := []byte(`
arbiter sales {
	poll 1s
	source https://feed.internal/facts
	on Qualified webhook https://hooks.internal/qualified
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

	makeWorkflow := func(t *testing.T) *Workflow {
		t.Helper()
		w, err := Compile(src, Options{})
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		return w
	}

	logPath := filepath.Join(t.TempDir(), "deliveries.jsonl")
	now := time.Unix(2_000, 0).UTC()
	webhookAttempts := 0
	handler := OutcomeHandlerFunc(func(_ context.Context, delivery Delivery) error {
		webhookAttempts++
		if delivery.Handler.Kind != "webhook" {
			t.Fatalf("unexpected handler kind %q", delivery.Handler.Kind)
		}
		if webhookAttempts == 1 {
			return errors.New("sink unavailable")
		}
		return nil
	})

	runner, err := NewRunner(makeWorkflow(t), RunnerOptions{
		Now: func() time.Time { return now },
		Loader: func(_ context.Context, _ string) ([]expert.Fact, error) {
			return []expert.Fact{{
				Type: "Lead",
				Key:  "lead-1",
				Fields: map[string]any{
					"score": float64(95),
				},
			}}, nil
		},
		Handlers: map[arbiter.ArbiterHandlerKind]OutcomeHandler{
			arbiter.ArbiterHandlerWebhook: handler,
		},
		DeliveryLog:    logPath,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     2 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRunner first: %v", err)
	}
	first, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	if first.Sinks["webhook\x00https://hooks.internal/qualified"].Pending != 1 {
		t.Fatalf("first sink state = %+v", first.Sinks)
	}
	if err := runner.Close(); err != nil {
		t.Fatalf("Close first runner: %v", err)
	}

	now = now.Add(10 * time.Second)
	runner, err = NewRunner(makeWorkflow(t), RunnerOptions{
		Now: func() time.Time { return now },
		Loader: func(_ context.Context, _ string) ([]expert.Fact, error) {
			return nil, nil
		},
		Handlers: map[arbiter.ArbiterHandlerKind]OutcomeHandler{
			arbiter.ArbiterHandlerWebhook: handler,
		},
		DeliveryLog:    logPath,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     2 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRunner second: %v", err)
	}
	second, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if webhookAttempts != 2 {
		t.Fatalf("webhookAttempts = %d, want 2", webhookAttempts)
	}
	if second.Sinks["webhook\x00https://hooks.internal/qualified"].Pending != 0 {
		t.Fatalf("second sink state = %+v", second.Sinks)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	logText := string(data)
	if !strings.Contains(logText, `"event":"queued"`) || !strings.Contains(logText, `"event":"failed"`) || !strings.Contains(logText, `"event":"delivered"`) {
		t.Fatalf("delivery log = %s", logText)
	}
}

func TestRunnerRestoresDispatchingDeliveriesAsAmbiguousWithoutReplay(t *testing.T) {
	src := []byte(`
arbiter sales {
	poll 1s
	source https://feed.internal/facts
	on Qualified webhook https://hooks.internal/qualified
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

	w, err := Compile(src, Options{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	logPath := filepath.Join(t.TempDir(), "deliveries.jsonl")
	delivery := Delivery{
		ID:         "1000-000001",
		Arbiter:    "sales",
		Handler:    arbiter.ArbiterHandler{Kind: arbiter.ArbiterHandlerWebhook, Target: "https://hooks.internal/qualified"},
		HandlerKey: "webhook\x00https://hooks.internal/qualified",
		Outcome: expert.Outcome{
			Name: "Qualified",
			Params: map[string]any{
				"key": "lead-1",
			},
		},
		EnqueuedAt:    time.Unix(4_000, 0).UTC(),
		LastAttemptAt: time.Unix(4_001, 0).UTC(),
	}
	writeDeliveryJournal(t, logPath,
		deliveryJournalEntry{Event: "queued", At: time.Unix(4_000, 0).UTC(), Delivery: delivery},
		deliveryJournalEntry{Event: "dispatching", At: time.Unix(4_001, 0).UTC(), Delivery: delivery},
	)

	attempts := 0
	runner, err := NewRunner(w, RunnerOptions{
		Loader: func(_ context.Context, _ string) ([]expert.Fact, error) { return nil, nil },
		Handlers: map[arbiter.ArbiterHandlerKind]OutcomeHandler{
			arbiter.ArbiterHandlerWebhook: OutcomeHandlerFunc(func(_ context.Context, _ Delivery) error {
				attempts++
				return nil
			}),
		},
		DeliveryLog: logPath,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	ambiguous := runner.AmbiguousDeliveries()
	if len(ambiguous) != 1 || ambiguous[0].ID != delivery.ID {
		t.Fatalf("ambiguous deliveries = %+v, want %q", ambiguous, delivery.ID)
	}

	sink := runner.SinkStates()[delivery.HandlerKey]
	if sink.Pending != 0 || sink.Ambiguous != 1 {
		t.Fatalf("sink state = %+v, want pending=0 ambiguous=1", sink)
	}

	tick, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if attempts != 0 {
		t.Fatalf("attempts = %d, want 0 for ambiguous delivery", attempts)
	}
	if tick.Delivered != 0 || tick.Retried != 0 || tick.Enqueued != 0 {
		t.Fatalf("tick delivery counts = %+v, want all zero", tick)
	}
	if got := runner.SinkStates()[delivery.HandlerKey].Ambiguous; got != 1 {
		t.Fatalf("ambiguous after tick = %d, want 1", got)
	}
}

func TestRunnerQueueDeliveryRespectsMaxPendingLimit(t *testing.T) {
	now := time.Unix(3_000, 0).UTC()
	runner := &Runner{
		now:                  func() time.Time { return now },
		pending:              make(map[string]Delivery),
		maxPendingDeliveries: 1,
	}

	if err := runner.queueDelivery(Delivery{EnqueuedAt: now}); err != nil {
		t.Fatalf("first queueDelivery: %v", err)
	}
	if err := runner.queueDelivery(Delivery{EnqueuedAt: now}); err == nil || !strings.Contains(err.Error(), "queue full") {
		t.Fatalf("second queueDelivery error = %v, want queue full", err)
	}
	if len(runner.pending) != 1 {
		t.Fatalf("pending len = %d, want 1", len(runner.pending))
	}
}

func TestRunnerQueueDeliveryRollsBackOnJournalError(t *testing.T) {
	tempDir := t.TempDir()
	blocker := filepath.Join(tempDir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	now := time.Unix(3_100, 0).UTC()
	runner := &Runner{
		now:         func() time.Time { return now },
		pending:     make(map[string]Delivery),
		deliveryLog: filepath.Join(blocker, "deliveries.jsonl"),
	}

	if err := runner.queueDelivery(Delivery{EnqueuedAt: now}); err == nil {
		t.Fatal("expected queueDelivery to fail when journal directory cannot be created")
	}
	if len(runner.pending) != 0 {
		t.Fatalf("pending len = %d, want 0 after journal failure", len(runner.pending))
	}
}

func TestRunnerDrainRetriesPendingDeliveriesUntilBackoffExpires(t *testing.T) {
	attempts := 0
	runner := &Runner{
		now:            time.Now,
		pending:        make(map[string]Delivery),
		sinks:          make(map[string]*sinkState),
		handlers:       make(map[arbiter.ArbiterHandlerKind]OutcomeHandler),
		initialBackoff: 5 * time.Millisecond,
		maxBackoff:     5 * time.Millisecond,
	}
	handlerKey := "webhook\x00https://hooks.internal/drain"
	runner.handlers[arbiter.ArbiterHandlerWebhook] = OutcomeHandlerFunc(func(_ context.Context, delivery Delivery) error {
		attempts++
		if delivery.Handler.Target != "https://hooks.internal/drain" {
			t.Fatalf("unexpected delivery target %q", delivery.Handler.Target)
		}
		if attempts == 1 {
			return errors.New("temporary failure")
		}
		return nil
	})
	runner.sinks[handlerKey] = &sinkState{
		SinkSnapshot: SinkSnapshot{
			Key:       handlerKey,
			Alias:     "hooks_internal_drain",
			Kind:      string(arbiter.ArbiterHandlerWebhook),
			Target:    "https://hooks.internal/drain",
			Available: true,
		},
	}

	now := time.Now().UTC()
	if err := runner.queueDelivery(Delivery{
		Handler: arbiter.ArbiterHandler{
			Kind:   arbiter.ArbiterHandlerWebhook,
			Target: "https://hooks.internal/drain",
		},
		HandlerKey: handlerKey,
		Outcome: expert.Outcome{
			Name: "Qualified",
		},
		EnqueuedAt: now,
	}); err != nil {
		t.Fatalf("queueDelivery: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	delivered, retried, err := runner.Drain(ctx)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if delivered != 1 || retried != 1 {
		t.Fatalf("Drain = delivered %d retried %d, want 1/1", delivered, retried)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if len(runner.pending) != 0 {
		t.Fatalf("pending len = %d, want 0", len(runner.pending))
	}
}

func TestRunnerDrainKeepsSinkSnapshotsReadableDuringBackoffWait(t *testing.T) {
	attempts := 0
	firstFailure := make(chan struct{})
	runner := &Runner{
		now:            time.Now,
		pending:        make(map[string]Delivery),
		sinks:          make(map[string]*sinkState),
		handlers:       make(map[arbiter.ArbiterHandlerKind]OutcomeHandler),
		initialBackoff: 200 * time.Millisecond,
		maxBackoff:     200 * time.Millisecond,
	}
	handlerKey := "webhook\x00https://hooks.internal/snapshot"
	runner.handlers[arbiter.ArbiterHandlerWebhook] = OutcomeHandlerFunc(func(_ context.Context, _ Delivery) error {
		attempts++
		if attempts == 1 {
			close(firstFailure)
			return errors.New("temporary failure")
		}
		return nil
	})
	runner.sinks[handlerKey] = &sinkState{
		SinkSnapshot: SinkSnapshot{
			Key:       handlerKey,
			Alias:     "hooks_internal_snapshot",
			Kind:      string(arbiter.ArbiterHandlerWebhook),
			Target:    "https://hooks.internal/snapshot",
			Available: true,
		},
	}

	now := time.Now().UTC()
	if err := runner.queueDelivery(Delivery{
		Handler: arbiter.ArbiterHandler{
			Kind:   arbiter.ArbiterHandlerWebhook,
			Target: "https://hooks.internal/snapshot",
		},
		HandlerKey: handlerKey,
		Outcome: expert.Outcome{
			Name: "Qualified",
		},
		EnqueuedAt: now,
	}); err != nil {
		t.Fatalf("queueDelivery: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	drainDone := make(chan error, 1)
	go func() {
		_, _, err := runner.Drain(ctx)
		drainDone <- err
	}()

	select {
	case <-firstFailure:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first delivery failure")
	}

	time.Sleep(10 * time.Millisecond)

	snapshotDone := make(chan map[string]SinkSnapshot, 1)
	go func() {
		snapshotDone <- runner.SinkStates()
	}()

	select {
	case sinks := <-snapshotDone:
		sink := sinks[handlerKey]
		if sink.Pending != 1 {
			t.Fatalf("sink pending = %d, want 1 while delivery is waiting for retry", sink.Pending)
		}
		if sink.Available {
			t.Fatalf("sink available = %v, want false after failed attempt", sink.Available)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("SinkStates blocked during drain backoff wait")
	}

	if err := <-drainDone; err != nil {
		t.Fatalf("Drain: %v", err)
	}
}

func TestRunnerRequeueAmbiguousMovesDeliveryBackToPending(t *testing.T) {
	handlerKey := "webhook\x00https://hooks.internal/requeue"
	delivery := Delivery{
		ID:         "5000-000001",
		Handler:    arbiter.ArbiterHandler{Kind: arbiter.ArbiterHandlerWebhook, Target: "https://hooks.internal/requeue"},
		HandlerKey: handlerKey,
		Outcome:    expert.Outcome{Name: "Qualified"},
		EnqueuedAt: time.Now().UTC(),
	}

	attempts := 0
	runner := &Runner{
		now:            time.Now,
		pending:        make(map[string]Delivery),
		ambiguous:      map[string]Delivery{delivery.ID: delivery},
		sinks:          map[string]*sinkState{handlerKey: {SinkSnapshot: SinkSnapshot{Key: handlerKey, Kind: string(arbiter.ArbiterHandlerWebhook), Target: "https://hooks.internal/requeue", Ambiguous: 1}}},
		handlers:       map[arbiter.ArbiterHandlerKind]OutcomeHandler{},
		initialBackoff: time.Millisecond,
		maxBackoff:     time.Millisecond,
	}
	runner.handlers[arbiter.ArbiterHandlerWebhook] = OutcomeHandlerFunc(func(_ context.Context, got Delivery) error {
		attempts++
		if got.ID != delivery.ID {
			t.Fatalf("delivery ID = %q, want %q", got.ID, delivery.ID)
		}
		return nil
	})

	requeued, err := runner.RequeueAmbiguous()
	if err != nil {
		t.Fatalf("RequeueAmbiguous: %v", err)
	}
	if requeued != 1 {
		t.Fatalf("requeued = %d, want 1", requeued)
	}
	sink := runner.SinkStates()[handlerKey]
	if sink.Pending != 1 || sink.Ambiguous != 0 {
		t.Fatalf("sink state after requeue = %+v, want pending=1 ambiguous=0", sink)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	delivered, retried, err := runner.Drain(ctx)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if delivered != 1 || retried != 0 {
		t.Fatalf("Drain = delivered %d retried %d, want 1/0", delivered, retried)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRunnerAcknowledgeAmbiguousRemovesDelivery(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "deliveries.jsonl")
	handlerKey := "webhook\x00https://hooks.internal/ack"
	delivery := Delivery{
		ID:         "6000-000001",
		Handler:    arbiter.ArbiterHandler{Kind: arbiter.ArbiterHandlerWebhook, Target: "https://hooks.internal/ack"},
		HandlerKey: handlerKey,
		Outcome:    expert.Outcome{Name: "Qualified"},
		EnqueuedAt: time.Now().UTC(),
	}
	writeDeliveryJournal(t, logPath,
		deliveryJournalEntry{Event: "queued", At: time.Unix(6_000, 0).UTC(), Delivery: delivery},
		deliveryJournalEntry{Event: "dispatching", At: time.Unix(6_001, 0).UTC(), Delivery: delivery},
	)

	runner := &Runner{
		now:         time.Now,
		pending:     make(map[string]Delivery),
		ambiguous:   map[string]Delivery{delivery.ID: delivery},
		sinks:       map[string]*sinkState{handlerKey: {SinkSnapshot: SinkSnapshot{Key: handlerKey, Kind: string(arbiter.ArbiterHandlerWebhook), Target: "https://hooks.internal/ack", Ambiguous: 1}}},
		deliveryLog: logPath,
	}

	acked, err := runner.AcknowledgeAmbiguous()
	if err != nil {
		t.Fatalf("AcknowledgeAmbiguous: %v", err)
	}
	if acked != 1 {
		t.Fatalf("acked = %d, want 1", acked)
	}
	if got := runner.SinkStates()[handlerKey].Ambiguous; got != 0 {
		t.Fatalf("ambiguous after acknowledge = %d, want 0", got)
	}

	restore := &Runner{
		now:       time.Now,
		pending:   make(map[string]Delivery),
		ambiguous: make(map[string]Delivery),
		sinks: map[string]*sinkState{
			handlerKey: {SinkSnapshot: SinkSnapshot{Key: handlerKey, Kind: string(arbiter.ArbiterHandlerWebhook), Target: "https://hooks.internal/ack"}},
		},
		deliveryLog: logPath,
	}
	if err := restore.restorePending(); err != nil {
		t.Fatalf("restorePending: %v", err)
	}
	restore.refreshSinkPendingCounts()
	if len(restore.AmbiguousDeliveries()) != 0 {
		t.Fatalf("restored ambiguous deliveries = %+v, want none", restore.AmbiguousDeliveries())
	}
	if sink := restore.SinkStates()[handlerKey]; sink.Pending != 0 || sink.Ambiguous != 0 {
		t.Fatalf("restored sink state = %+v, want empty", sink)
	}
}

func TestRunnerFiltersWebhookDeliveriesByOutcomeFields(t *testing.T) {
	src := []byte(`
arbiter alerts {
	poll 1s
	source https://feed.internal/alerts
	on Alert where severity == "critical" webhook https://hooks.internal/critical
}

expert rule EmitAlerts priority 10 per_fact {
	when {
		any candidate in facts.InputAlert { true }
	}
	then emit Alert {
		key: candidate.key,
		severity: candidate.severity,
		channel: candidate.channel,
	}
}
`)

	w, err := Compile(src, Options{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	var deliveries []Delivery
	runner, err := NewRunner(w, RunnerOptions{
		Loader: func(_ context.Context, target string) ([]expert.Fact, error) {
			if target != "https://feed.internal/alerts" {
				t.Fatalf("unexpected target %q", target)
			}
			return []expert.Fact{
				{
					Type: "InputAlert",
					Key:  "alert-1",
					Fields: map[string]any{
						"severity": "critical",
						"channel":  "incidents",
					},
				},
				{
					Type: "InputAlert",
					Key:  "alert-2",
					Fields: map[string]any{
						"severity": "warning",
						"channel":  "warnings",
					},
				},
			}, nil
		},
		Handlers: map[arbiter.ArbiterHandlerKind]OutcomeHandler{
			arbiter.ArbiterHandlerWebhook: OutcomeHandlerFunc(func(_ context.Context, delivery Delivery) error {
				deliveries = append(deliveries, delivery)
				return nil
			}),
		},
		InitialBackoff: time.Millisecond,
		MaxBackoff:     2 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	tick, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if tick.Enqueued != 1 || tick.Delivered != 1 {
		t.Fatalf("tick delivery counts = %+v, want enqueued=1 delivered=1", tick)
	}
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %+v, want 1 critical delivery", deliveries)
	}
	if got := deliveries[0].Outcome.Name; got != "Alert" {
		t.Fatalf("delivery outcome = %q, want Alert", got)
	}
	if got := deliveries[0].Outcome.Params["severity"]; got != "critical" {
		t.Fatalf("delivery severity = %#v, want critical", got)
	}
	if got := deliveries[0].Outcome.Params["channel"]; got != "incidents" {
		t.Fatalf("delivery channel = %#v, want incidents", got)
	}
	sink := tick.Sinks["webhook\x00https://hooks.internal/critical"]
	if sink.Pending != 0 || !sink.Available {
		t.Fatalf("sink state = %+v, want available with no backlog", sink)
	}
}

func TestRunnerTickKeepsSinkSnapshotsReadableDuringSlowDelivery(t *testing.T) {
	src := []byte(`
arbiter sales {
	poll 1s
	source https://feed.internal/facts
	on Qualified webhook https://hooks.internal/qualified
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

	w, err := Compile(src, Options{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	deliveryStarted := make(chan struct{})
	releaseDelivery := make(chan struct{})
	runner, err := NewRunner(w, RunnerOptions{
		Loader: func(_ context.Context, _ string) ([]expert.Fact, error) {
			return []expert.Fact{{
				Type: "Lead",
				Key:  "lead-1",
				Fields: map[string]any{
					"score": float64(95),
				},
			}}, nil
		},
		Handlers: map[arbiter.ArbiterHandlerKind]OutcomeHandler{
			arbiter.ArbiterHandlerWebhook: OutcomeHandlerFunc(func(_ context.Context, delivery Delivery) error {
				if delivery.Handler.Target != "https://hooks.internal/qualified" {
					t.Fatalf("unexpected delivery target %q", delivery.Handler.Target)
				}
				close(deliveryStarted)
				<-releaseDelivery
				return nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	tickDone := make(chan error, 1)
	go func() {
		_, err := runner.Tick(context.Background())
		tickDone <- err
	}()

	select {
	case <-deliveryStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delivery to start")
	}

	snapshotDone := make(chan map[string]SinkSnapshot, 1)
	go func() {
		snapshotDone <- runner.SinkStates()
	}()

	select {
	case sinks := <-snapshotDone:
		sink, ok := sinks["webhook\x00https://hooks.internal/qualified"]
		if !ok {
			t.Fatalf("sink snapshot missing target: %+v", sinks)
		}
		if sink.Target != "https://hooks.internal/qualified" {
			t.Fatalf("unexpected sink snapshot: %+v", sink)
		}
		if sink.Pending != 1 {
			t.Fatalf("sink pending = %d, want 1 during slow delivery", sink.Pending)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("SinkStates blocked during slow delivery")
	}

	close(releaseDelivery)
	if err := <-tickDone; err != nil {
		t.Fatalf("Tick: %v", err)
	}
}

func TestRunnerDispatchesWorkersThroughUnderlyingCapability(t *testing.T) {
	src := []byte(`
outcome Qualified {
	key: string
}

outcome ExecutionResult {
	status: string
}

worker notify_sales {
	input Qualified
	output ExecutionResult
	webhook https://hooks.internal/qualified
}

arbiter sales {
	poll 1s
	source https://feed.internal/facts
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

	w, err := Compile(src, Options{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	deliveries := 0
	handler := OutcomeHandlerFunc(func(_ context.Context, delivery Delivery) error {
		deliveries++
		if delivery.Worker != "notify_sales" {
			t.Fatalf("unexpected worker name %q", delivery.Worker)
		}
		if delivery.Handler.Kind != arbiter.ArbiterHandlerWebhook || delivery.Handler.Target != "https://hooks.internal/qualified" {
			t.Fatalf("unexpected resolved handler: %+v", delivery.Handler)
		}
		if delivery.Outcome.Name != "Qualified" || delivery.Outcome.Params["key"] != "lead-1" {
			t.Fatalf("unexpected outcome: %+v", delivery.Outcome)
		}
		return nil
	})

	runner, err := NewRunner(w, RunnerOptions{
		Loader: func(_ context.Context, _ string) ([]expert.Fact, error) {
			return []expert.Fact{{
				Type: "Lead",
				Key:  "lead-1",
				Fields: map[string]any{
					"score": float64(95),
				},
			}}, nil
		},
		Handlers: map[arbiter.ArbiterHandlerKind]OutcomeHandler{
			arbiter.ArbiterHandlerWebhook: handler,
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	tick, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if deliveries != 1 {
		t.Fatalf("deliveries = %d, want 1", deliveries)
	}
	sink := tick.Sinks["worker\x00notify_sales"]
	if sink.Kind != "worker" || sink.Target != "https://hooks.internal/qualified" || sink.Pending != 0 {
		t.Fatalf("unexpected worker sink snapshot: %+v", sink)
	}
}

func TestRunnerFeedsWorkerFactOutputsBackThroughWorkerSources(t *testing.T) {
	src := []byte(`
fact WorkerReceipt {
	status: string
}

outcome Qualified {
	key: string
}

outcome ReceiptObserved {
	key: string
	status: string
}

worker notify_sales {
	input Qualified
	output WorkerReceipt
	webhook https://hooks.internal/qualified
}

arbiter sales {
	poll 1s
	source https://feed.internal/facts
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

expert rule ObserveReceipt priority 20 per_fact {
	when {
		any receipt in facts.WorkerReceipt { receipt.status == "sent" }
	}
	then emit ReceiptObserved {
		key: receipt.key,
		status: receipt.status,
	}
}
`)

	w, err := Compile(src, Options{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	invocations := 0
	runner, err := NewRunner(w, RunnerOptions{
		Loader: func(_ context.Context, _ string) ([]expert.Fact, error) {
			return []expert.Fact{{
				Type: "Lead",
				Key:  "lead-1",
				Fields: map[string]any{
					"score": float64(95),
				},
			}}, nil
		},
		WorkerHandlers: map[arbiter.ArbiterHandlerKind]WorkerHandler{
			arbiter.ArbiterHandlerWebhook: WorkerHandlerFunc(func(_ context.Context, invocation WorkerInvocation) (WorkerExecution, error) {
				invocations++
				if invocation.Worker.Name != "notify_sales" {
					t.Fatalf("unexpected worker invocation %+v", invocation.Worker)
				}
				return WorkerExecution{
					Facts: []expert.Fact{{
						Key: "lead-1",
						Fields: map[string]any{
							"status": "sent",
						},
					}},
				}, nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	first, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	if invocations != 1 {
		t.Fatalf("worker invocations = %d, want 1", invocations)
	}
	workerSource := first.Sources["worker://notify_sales"]
	if !workerSource.Available || workerSource.FactCount != 1 {
		t.Fatalf("unexpected worker source snapshot after first tick: %+v", workerSource)
	}
	if got := first.Workflow.Arbiters["sales"].Delta.Outcomes; len(got) != 1 || got[0].Name != "Qualified" {
		t.Fatalf("first delta outcomes = %+v, want Qualified only", got)
	}

	second, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	got := second.Workflow.Arbiters["sales"].Delta.Outcomes
	if len(got) != 1 || got[0].Name != "ReceiptObserved" {
		t.Fatalf("second delta outcomes = %+v, want ReceiptObserved", got)
	}
	if got[0].Params["status"] != "sent" || got[0].Params["key"] != "lead-1" {
		t.Fatalf("unexpected receipt outcome params: %+v", got[0].Params)
	}
}

func TestRunnerMaterializesWorkerOutcomeOutputsIntoWorkerSources(t *testing.T) {
	src := []byte(`
outcome Qualified {
	key: string
}

outcome ExecutionResult {
	key: string
	status: string
}

outcome ExecutionObserved {
	key: string
	status: string
}

worker notify_sales {
	input Qualified
	output ExecutionResult
	webhook https://hooks.internal/qualified
}

arbiter sales {
	poll 1s
	source https://feed.internal/facts
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

expert rule ObserveExecution priority 20 per_fact {
	when {
		any result in facts.ExecutionResult { result.status == "sent" }
	}
	then emit ExecutionObserved {
		key: result.key,
		status: result.status,
	}
}
`)

	w, err := Compile(src, Options{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	runner, err := NewRunner(w, RunnerOptions{
		Loader: func(_ context.Context, _ string) ([]expert.Fact, error) {
			return []expert.Fact{{
				Type: "Lead",
				Key:  "lead-1",
				Fields: map[string]any{
					"score": float64(95),
				},
			}}, nil
		},
		WorkerHandlers: map[arbiter.ArbiterHandlerKind]WorkerHandler{
			arbiter.ArbiterHandlerWebhook: WorkerHandlerFunc(func(_ context.Context, invocation WorkerInvocation) (WorkerExecution, error) {
				return WorkerExecution{
					Outcomes: []expert.Outcome{{
						Params: map[string]any{
							"key":    invocation.Delivery.Outcome.Params["key"],
							"status": "sent",
						},
					}},
				}, nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	second, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	got := second.Workflow.Arbiters["sales"].Delta.Outcomes
	if len(got) != 1 || got[0].Name != "ExecutionObserved" {
		t.Fatalf("second delta outcomes = %+v, want ExecutionObserved", got)
	}
	if got[0].Params["status"] != "sent" || got[0].Params["key"] != "lead-1" {
		t.Fatalf("unexpected execution outcome params: %+v", got[0].Params)
	}
}

func TestRunnerExposesSinkFailuresToRulesOnNextTick(t *testing.T) {
	src := []byte(`
arbiter sales {
	poll 1s
	source https://feed.internal/facts
	on Qualified webhook https://hooks.internal/qualified
}

expert rule EmitQualified priority 10 per_fact {
	when {
		any lead in facts.Lead { lead.score >= 90 }
	}
	then emit Qualified {
		key: lead.key,
	}
}

expert rule ObserveSinkFailure priority 20 {
	when {
		sink.hooks_internal_qualified.available == false
		and sink.hooks_internal_qualified.pending >= 1
	}
	then emit SinkUnavailable {
		pending: sink.hooks_internal_qualified.pending,
		failures: sink.hooks_internal_qualified.consecutive_failures,
	}
}
`)

	w, err := Compile(src, Options{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	attempts := 0
	now := time.Unix(3_000, 0).UTC()
	runner, err := NewRunner(w, RunnerOptions{
		Now: func() time.Time { return now },
		Loader: func(_ context.Context, _ string) ([]expert.Fact, error) {
			return []expert.Fact{{
				Type: "Lead",
				Key:  "lead-1",
				Fields: map[string]any{
					"score": float64(95),
				},
			}}, nil
		},
		Handlers: map[arbiter.ArbiterHandlerKind]OutcomeHandler{
			arbiter.ArbiterHandlerWebhook: OutcomeHandlerFunc(func(_ context.Context, delivery Delivery) error {
				attempts++
				if delivery.Outcome.Name != "Qualified" {
					t.Fatalf("unexpected delivery outcome %q", delivery.Outcome.Name)
				}
				if attempts == 1 {
					return errors.New("temporary sink failure")
				}
				return nil
			}),
		},
		InitialBackoff: time.Millisecond,
		MaxBackoff:     2 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	first, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	if first.Retried != 1 {
		t.Fatalf("first retried = %d, want 1", first.Retried)
	}
	firstSink := first.Sinks["webhook\x00https://hooks.internal/qualified"]
	if firstSink.Pending != 1 || firstSink.Available {
		t.Fatalf("first sink state = %+v, want pending failed delivery", firstSink)
	}

	now = now.Add(10 * time.Millisecond)
	second, err := runner.Tick(context.Background())
	if err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("webhook attempts = %d, want 2", attempts)
	}
	if second.Delivered != 1 {
		t.Fatalf("second delivered = %d, want 1", second.Delivered)
	}
	var sawSinkUnavailable bool
	for _, outcome := range second.Workflow.Arbiters["sales"].Delta.Outcomes {
		if outcome.Name != "SinkUnavailable" {
			continue
		}
		sawSinkUnavailable = true
		if got := outcome.Params["pending"]; got != float64(1) {
			t.Fatalf("pending = %#v, want 1", got)
		}
		if got := outcome.Params["failures"]; got != float64(1) {
			t.Fatalf("failures = %#v, want 1", got)
		}
	}
	if !sawSinkUnavailable {
		t.Fatalf("second outcomes = %+v, want SinkUnavailable", second.Workflow.Arbiters["sales"].Delta.Outcomes)
	}
	secondSink := second.Sinks["webhook\x00https://hooks.internal/qualified"]
	if secondSink.Pending != 0 || !secondSink.Available {
		t.Fatalf("second sink state = %+v, want recovered sink", secondSink)
	}
}
