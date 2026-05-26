package workflow

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	arbiter "m31labs.dev/arbiter"
	"m31labs.dev/arbiter/audit"
	"m31labs.dev/arbiter/expert"
)

type deliveryAttemptGroup struct {
	handlerKey string
	ids        []string
}

type deliveryAttemptResult struct {
	delivered int
	retried   int
	err       error
}

func (r *Runner) enqueueWorkflowDeliveries(result Result) (int, error) {
	enqueued := 0
	now := r.now().UTC()
	for arbiterName, run := range result.Arbiters {
		count, err := r.enqueueArbiterDeliveries(arbiterName, run.Delta.Outcomes, now)
		if err != nil {
			return enqueued, err
		}
		enqueued += count
	}
	return enqueued, nil
}

func (r *Runner) deliverReady(ctx context.Context) (delivered int, retried int, err error) {
	now := r.now().UTC()
	groups := r.readyDeliveryGroups(now)
	if len(groups) == 0 {
		return 0, 0, nil
	}
	results := make([]deliveryAttemptResult, len(groups))
	limit := parallelismLimit(r.maxConcurrentDeliveries, len(groups))
	if limit == 1 {
		for i, group := range groups {
			results[i] = r.processDeliveryGroup(ctx, group, now)
		}
	} else {
		sem := make(chan struct{}, limit)
		var wg sync.WaitGroup
		for i, group := range groups {
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					results[i] = deliveryAttemptResult{err: ctx.Err()}
					return
				}
				defer func() { <-sem }()
				results[i] = r.processDeliveryGroup(ctx, group, now)
			}()
		}
		wg.Wait()
	}
	for _, result := range results {
		delivered += result.delivered
		retried += result.retried
		if err == nil && result.err != nil {
			err = result.err
		}
	}
	return delivered, retried, err
}

func (r *Runner) readyDeliveryGroups(now time.Time) []deliveryAttemptGroup {
	ids := r.pendingDeliveryIDs()
	if len(ids) == 0 {
		return nil
	}
	groupIndex := make(map[string]int)
	groups := make([]deliveryAttemptGroup, 0, len(ids))
	for _, id := range ids {
		delivery, ok := r.pendingDelivery(id)
		if !ok || !deliveryReady(now, delivery) {
			continue
		}
		index, ok := groupIndex[delivery.HandlerKey]
		if !ok {
			index = len(groups)
			groupIndex[delivery.HandlerKey] = index
			groups = append(groups, deliveryAttemptGroup{handlerKey: delivery.HandlerKey})
		}
		groups[index].ids = append(groups[index].ids, id)
	}
	return groups
}

func (r *Runner) processDeliveryGroup(ctx context.Context, group deliveryAttemptGroup, now time.Time) deliveryAttemptResult {
	result := deliveryAttemptResult{}
	for _, id := range group.ids {
		if err := ctx.Err(); err != nil {
			result.err = err
			return result
		}
		stillPending, err := r.processDeliveryAttempt(ctx, id, now)
		if err != nil {
			result.err = err
			return result
		}
		if stillPending {
			result.retried++
			continue
		}
		result.delivered++
	}
	return result
}

func (r *Runner) enqueueArbiterDeliveries(arbiterName string, outcomes []expert.Outcome, now time.Time) (int, error) {
	enqueued := 0
	for _, handler := range r.dispatchers[arbiterName] {
		count, err := r.enqueueHandlerDeliveries(arbiterName, handler, outcomes, now)
		if err != nil {
			return enqueued, err
		}
		enqueued += count
	}
	return enqueued, nil
}

func (r *Runner) enqueueHandlerDeliveries(arbiterName string, handler compiledDispatchHandler, outcomes []expert.Outcome, now time.Time) (int, error) {
	enqueued := 0
	for _, outcome := range outcomes {
		matched, err := handlerMatchesOutcome(arbiterName, handler, outcome)
		if err != nil {
			return enqueued, err
		}
		if !matched {
			continue
		}
		if err := r.queueDelivery(newDelivery(now, arbiterName, handler, outcome)); err != nil {
			return enqueued, err
		}
		enqueued++
	}
	return enqueued, nil
}

func handlerMatchesOutcome(arbiterName string, handler compiledDispatchHandler, outcome expert.Outcome) (bool, error) {
	if handler.spec.Outcome != "*" && handler.spec.Outcome != outcome.Name {
		return false, nil
	}
	ok, err := handler.filter.Match(outcome)
	if err != nil {
		return false, fmt.Errorf("workflow handler %s %s: %w", arbiterName, handler.spec.Kind, err)
	}
	return ok, nil
}

func newDelivery(now time.Time, arbiterName string, handler compiledDispatchHandler, outcome expert.Outcome) Delivery {
	delivery := Delivery{
		Arbiter:    arbiterName,
		Handler:    handler.spec,
		HandlerKey: handler.handlerKey,
		Outcome: expert.Outcome{
			Rule:   outcome.Rule,
			Name:   outcome.Name,
			Params: cloneMap(outcome.Params),
		},
		EnqueuedAt: now,
	}
	if handler.worker != nil {
		delivery.Worker = handler.worker.Name
	}
	return delivery
}

func (r *Runner) queueDelivery(delivery Delivery) error {
	r.mu.Lock()
	if r.maxPendingDeliveries > 0 && len(r.pending) >= r.maxPendingDeliveries {
		r.mu.Unlock()
		return fmt.Errorf("workflow pending delivery queue full: limit %d", r.maxPendingDeliveries)
	}
	delivery.ID = r.nextDeliveryID(delivery.EnqueuedAt)
	r.pending[delivery.ID] = delivery
	r.adjustSinkPendingLocked(delivery.HandlerKey, 1)
	r.mu.Unlock()
	if err := r.appendJournal("queued", delivery); err != nil {
		r.mu.Lock()
		delete(r.pending, delivery.ID)
		r.adjustSinkPendingLocked(delivery.HandlerKey, -1)
		r.mu.Unlock()
		return err
	}
	return nil
}

func (r *Runner) pendingDeliveryIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.pending))
	for id := range r.pending {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func (r *Runner) pendingDelivery(id string) (Delivery, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	delivery, ok := r.pending[id]
	return delivery, ok
}

func deliveryReady(now time.Time, delivery Delivery) bool {
	return delivery.NextAttemptAt.IsZero() || !now.Before(delivery.NextAttemptAt)
}

func (r *Runner) processDeliveryAttempt(ctx context.Context, id string, now time.Time) (bool, error) {
	delivery, ok := r.beginDeliveryAttempt(id, now)
	if !ok {
		return false, nil
	}
	if err := r.appendJournal("dispatching", delivery); err != nil {
		return true, err
	}
	if err := r.dispatch(ctx, delivery); err != nil {
		return true, r.recordDeliveryFailure(id, delivery, now, err)
	}
	return false, r.recordDeliverySuccess(id, delivery, now)
}

func (r *Runner) beginDeliveryAttempt(id string, now time.Time) (Delivery, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delivery, ok := r.pending[id]
	if !ok {
		return Delivery{}, false
	}
	state := r.sinks[delivery.HandlerKey]
	if state != nil {
		state.LastAttemptAt = now
	}
	delivery.LastAttemptAt = now
	r.pending[id] = delivery
	return delivery, true
}

func (r *Runner) recordDeliveryFailure(id string, delivery Delivery, now time.Time, err error) error {
	delivery.Attempt++
	delivery.LastError = err.Error()
	delivery.NextAttemptAt = now.Add(deliveryBackoff(delivery.Attempt, r.initialBackoff, r.maxBackoff))
	r.mu.Lock()
	r.pending[id] = delivery
	if delivery.Worker != "" {
		r.markWorkerSourceFailureLocked(delivery.Worker, now, err)
	}
	state := r.sinks[delivery.HandlerKey]
	if state != nil {
		state.Available = false
		state.LastError = delivery.LastError
		state.ConsecutiveFailures++
		state.NextRetryAt = delivery.NextAttemptAt
	}
	r.mu.Unlock()
	return r.appendJournal("failed", delivery)
}

func (r *Runner) recordDeliverySuccess(id string, delivery Delivery, now time.Time) error {
	r.mu.Lock()
	delete(r.pending, id)
	r.adjustSinkPendingLocked(delivery.HandlerKey, -1)
	state := r.sinks[delivery.HandlerKey]
	if state != nil {
		state.Available = true
		state.LastError = ""
		state.ConsecutiveFailures = 0
		state.LastSuccessAt = now
		state.NextRetryAt = time.Time{}
	}
	r.mu.Unlock()
	return r.appendJournal("delivered", delivery)
}

func (r *Runner) adjustSinkPendingLocked(handlerKey string, delta int) {
	state := r.sinks[handlerKey]
	if state == nil || delta == 0 {
		return
	}
	state.Pending += delta
	if state.Pending < 0 {
		state.Pending = 0
	}
}

func (r *Runner) adjustSinkAmbiguousLocked(handlerKey string, delta int) {
	state := r.sinks[handlerKey]
	if state == nil || delta == 0 {
		return
	}
	state.Ambiguous += delta
	if state.Ambiguous < 0 {
		state.Ambiguous = 0
	}
}

func (r *Runner) dispatch(ctx context.Context, delivery Delivery) error {
	switch delivery.Handler.Kind {
	case arbiter.ArbiterHandlerAudit:
		return r.deliverAudit(ctx, delivery)
	case arbiter.ArbiterHandlerStdout:
		return r.deliverStdout(ctx, delivery)
	case arbiter.ArbiterHandlerWorker:
		return r.dispatchWorker(ctx, delivery)
	default:
		handler := r.handlers[delivery.Handler.Kind]
		if handler == nil {
			return fmt.Errorf("no runtime handler for %s", delivery.Handler.Kind)
		}
		return handler.Deliver(ctx, delivery)
	}
}

func (r *Runner) dispatchWorker(ctx context.Context, delivery Delivery) error {
	worker, ok := r.workflow.workers[delivery.Handler.Target]
	if !ok {
		return fmt.Errorf("unknown worker %q", delivery.Handler.Target)
	}
	resolved := delivery
	resolved.Worker = worker.Name
	resolved.Handler = arbiter.ArbiterHandler{
		Outcome: delivery.Handler.Outcome,
		Where:   delivery.Handler.Where,
		Kind:    worker.Kind,
		Target:  worker.Target,
	}
	handler := r.workerHandlers[worker.Kind]
	if handler == nil {
		return fmt.Errorf("no worker runtime registered for %s", worker.Kind)
	}
	result, err := handler.Execute(ctx, WorkerInvocation{
		Worker:   worker,
		Delivery: resolved,
	})
	if err != nil {
		return err
	}
	return r.applyWorkerExecution(delivery.Arbiter, worker, result)
}

func (r *Runner) deliverAudit(ctx context.Context, delivery Delivery) error {
	sink, err := r.auditSink(delivery.Handler.Target)
	if err != nil {
		return err
	}
	return sink.WriteDecision(ctx, audit.DecisionEvent{
		Timestamp: delivery.EnqueuedAt,
		Kind:      "arbiter_outcome",
		Context: map[string]any{
			"arbiter": delivery.Arbiter,
			"target":  delivery.Handler.Target,
			"handler": string(delivery.Handler.Kind),
		},
		Expert: &audit.ExpertDecision{
			Outcomes: []audit.ExpertOutcome{{
				Rule:   delivery.Outcome.Rule,
				Name:   delivery.Outcome.Name,
				Params: cloneMap(delivery.Outcome.Params),
			}},
		},
	})
}

func (r *Runner) deliverStdout(_ context.Context, delivery Delivery) error {
	r.stdoutMu.Lock()
	defer r.stdoutMu.Unlock()
	enc := json.NewEncoder(r.stdout)
	return enc.Encode(delivery)
}

func (r *Runner) auditSink(target string) (*audit.JSONLSink, error) {
	if target == "" {
		return nil, fmt.Errorf("audit handler target is required")
	}
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	if sink := r.auditSinks[target]; sink != nil {
		return sink, nil
	}
	sink, err := audit.NewJSONLSink(target)
	if err != nil {
		return nil, err
	}
	r.auditSinks[target] = sink
	return sink, nil
}

func (r *Runner) restorePending() error {
	if strings.TrimSpace(r.deliveryLog) == "" {
		return nil
	}
	file, err := os.Open(r.deliveryLog)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("workflow delivery log open: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry deliveryJournalEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return fmt.Errorf("workflow delivery log decode: %w", err)
		}
		switch entry.Event {
		case "queued", "failed":
			r.pending[entry.Delivery.ID] = entry.Delivery
			delete(r.ambiguous, entry.Delivery.ID)
		case "dispatching":
			delete(r.pending, entry.Delivery.ID)
			r.ambiguous[entry.Delivery.ID] = entry.Delivery
		case "delivered":
			delete(r.pending, entry.Delivery.ID)
			delete(r.ambiguous, entry.Delivery.ID)
		case "acknowledged":
			delete(r.pending, entry.Delivery.ID)
			delete(r.ambiguous, entry.Delivery.ID)
		}
		if entry.Delivery.ID != "" {
			r.nextID++
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("workflow delivery log read: %w", err)
	}
	if r.maxPendingDeliveries > 0 && len(r.pending) > r.maxPendingDeliveries {
		return fmt.Errorf("workflow pending delivery queue full: restored %d deliveries exceeds limit %d", len(r.pending), r.maxPendingDeliveries)
	}
	return nil
}

func (r *Runner) appendJournal(event string, delivery Delivery) error {
	if strings.TrimSpace(r.deliveryLog) == "" {
		return nil
	}
	r.logMu.Lock()
	defer r.logMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(r.deliveryLog), 0o755); err != nil {
		return fmt.Errorf("workflow delivery log mkdir: %w", err)
	}
	file, err := os.OpenFile(r.deliveryLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("workflow delivery log open: %w", err)
	}
	defer file.Close()

	entry := deliveryJournalEntry{
		Event:    event,
		At:       r.now().UTC(),
		Delivery: delivery,
	}
	if err := json.NewEncoder(file).Encode(entry); err != nil {
		return fmt.Errorf("workflow delivery log write: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("workflow delivery log sync: %w", err)
	}
	return nil
}

func (r *Runner) nextDeliveryID(now time.Time) string {
	r.nextID++
	return fmt.Sprintf("%d-%06d", now.UTC().UnixNano(), r.nextID)
}

func (r *Runner) refreshSinkPendingCounts() {
	for _, state := range r.sinks {
		state.Pending = 0
		state.Ambiguous = 0
	}
	for _, delivery := range r.pending {
		if state := r.sinks[delivery.HandlerKey]; state != nil {
			state.Pending++
		}
	}
	for _, delivery := range r.ambiguous {
		if state := r.sinks[delivery.HandlerKey]; state != nil {
			state.Ambiguous++
		}
	}
}

// RequeueAmbiguous moves ambiguous deliveries back into the pending queue so
// they can be retried explicitly. If no ids are provided, all ambiguous
// deliveries are requeued.
func (r *Runner) RequeueAmbiguous(ids ...string) (int, error) {
	if r == nil {
		return 0, nil
	}
	r.execMu.Lock()
	defer r.execMu.Unlock()

	selected, err := r.selectAmbiguous(ids...)
	if err != nil {
		return 0, err
	}

	requeued := 0
	for _, original := range selected {
		delivery := cloneDelivery(original)
		delivery.NextAttemptAt = time.Time{}
		delivery.LastError = "requeued after ambiguous delivery recovery"

		r.mu.Lock()
		if r.maxPendingDeliveries > 0 && len(r.pending) >= r.maxPendingDeliveries {
			r.mu.Unlock()
			return requeued, fmt.Errorf("workflow pending delivery queue full: limit %d", r.maxPendingDeliveries)
		}
		delete(r.ambiguous, delivery.ID)
		r.adjustSinkAmbiguousLocked(delivery.HandlerKey, -1)
		r.pending[delivery.ID] = delivery
		r.adjustSinkPendingLocked(delivery.HandlerKey, 1)
		r.mu.Unlock()

		if err := r.appendJournal("queued", delivery); err != nil {
			r.mu.Lock()
			delete(r.pending, delivery.ID)
			r.adjustSinkPendingLocked(delivery.HandlerKey, -1)
			r.ambiguous[delivery.ID] = original
			r.adjustSinkAmbiguousLocked(delivery.HandlerKey, 1)
			r.mu.Unlock()
			return requeued, err
		}
		requeued++
	}
	return requeued, nil
}

// AcknowledgeAmbiguous removes ambiguous deliveries without replaying them. If
// no ids are provided, all ambiguous deliveries are acknowledged.
func (r *Runner) AcknowledgeAmbiguous(ids ...string) (int, error) {
	if r == nil {
		return 0, nil
	}
	r.execMu.Lock()
	defer r.execMu.Unlock()

	selected, err := r.selectAmbiguous(ids...)
	if err != nil {
		return 0, err
	}

	acked := 0
	for _, delivery := range selected {
		r.mu.Lock()
		delete(r.ambiguous, delivery.ID)
		r.adjustSinkAmbiguousLocked(delivery.HandlerKey, -1)
		r.mu.Unlock()

		if err := r.appendJournal("acknowledged", delivery); err != nil {
			r.mu.Lock()
			r.ambiguous[delivery.ID] = delivery
			r.adjustSinkAmbiguousLocked(delivery.HandlerKey, 1)
			r.mu.Unlock()
			return acked, err
		}
		acked++
	}
	return acked, nil
}

func (r *Runner) selectAmbiguous(ids ...string) ([]Delivery, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(ids) == 0 {
		ids = make([]string, 0, len(r.ambiguous))
		for id := range r.ambiguous {
			ids = append(ids, id)
		}
		slices.Sort(ids)
	}

	seen := make(map[string]struct{}, len(ids))
	selected := make([]Delivery, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		delivery, ok := r.ambiguous[id]
		if !ok {
			return nil, fmt.Errorf("unknown ambiguous delivery %q", id)
		}
		selected = append(selected, cloneDelivery(delivery))
	}
	return selected, nil
}

func cloneDelivery(delivery Delivery) Delivery {
	delivery.Outcome = expert.Outcome{
		Rule:   delivery.Outcome.Rule,
		Name:   delivery.Outcome.Name,
		Params: cloneMap(delivery.Outcome.Params),
	}
	return delivery
}

func sinkHandlerKey(spec arbiter.ArbiterHandler) string {
	if spec.Kind == arbiter.ArbiterHandlerStdout {
		return string(spec.Kind)
	}
	if spec.Kind == arbiter.ArbiterHandlerWorker {
		return string(spec.Kind) + "\x00" + spec.Target
	}
	return string(spec.Kind) + "\x00" + spec.Target
}

func deliveryBackoff(attempt int, initial, max time.Duration) time.Duration {
	if attempt <= 0 {
		return initial
	}
	backoff := initial
	for i := 1; i < attempt; i++ {
		backoff = nextBackoff(backoff, max)
	}
	return backoff
}

func nextBackoff(current, max time.Duration) time.Duration {
	if current <= 0 {
		current = time.Second
	}
	next := current * 2
	if next > max {
		return max
	}
	return next
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
