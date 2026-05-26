package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"m31labs.dev/arbiter/audit"
	"m31labs.dev/arbiter/observability"
)

type controlAuditTracker struct {
	mu sync.RWMutex

	configured bool
	kind       string
	durable    bool
	file       string

	writesTotal   uint64
	errorsTotal   uint64
	lastSuccessAt time.Time
	lastError     string
	lastErrorAt   time.Time
}

func newControlAuditTracker(configured bool, kind string, durable bool, file string) *controlAuditTracker {
	return &controlAuditTracker{
		configured: configured,
		kind:       kind,
		durable:    durable,
		file:       file,
	}
}

func (t *controlAuditTracker) Snapshot() controlAuditStatus {
	if t == nil {
		return controlAuditStatus{
			Configured: false,
			Kind:       "discard",
			Durable:    false,
			Healthy:    true,
		}
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	if !t.configured {
		return controlAuditStatus{
			Configured: false,
			Kind:       "discard",
			Durable:    false,
			Healthy:    true,
		}
	}

	healthy := true
	if !t.lastErrorAt.IsZero() && (t.lastSuccessAt.IsZero() || t.lastErrorAt.After(t.lastSuccessAt)) {
		healthy = false
	}

	return controlAuditStatus{
		Configured:    t.configured,
		Kind:          t.kind,
		Durable:       t.durable,
		File:          t.file,
		Healthy:       healthy,
		WritesTotal:   t.writesTotal,
		ErrorsTotal:   t.errorsTotal,
		LastSuccessAt: t.lastSuccessAt,
		LastError:     t.lastError,
		LastErrorAt:   t.lastErrorAt,
	}
}

func (t *controlAuditTracker) recordSuccess(at time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.configured {
		return
	}
	t.writesTotal++
	t.lastSuccessAt = at.UTC()
}

func (t *controlAuditTracker) recordError(at time.Time, err error) {
	if t == nil || err == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.configured {
		return
	}
	t.errorsTotal++
	t.lastError = err.Error()
	t.lastErrorAt = at.UTC()
}

type trackedAuditSink struct {
	inner   audit.Sink
	tracker *controlAuditTracker
	logger  *slog.Logger
}

func newTrackedAuditSink(inner audit.Sink, tracker *controlAuditTracker, logger *slog.Logger) audit.Sink {
	if inner == nil {
		inner = audit.NopSink{}
	}
	return trackedAuditSink{
		inner:   inner,
		tracker: tracker,
		logger:  logger,
	}
}

func (s trackedAuditSink) WriteDecision(ctx context.Context, event audit.DecisionEvent) error {
	err := s.inner.WriteDecision(ctx, event)
	now := time.Now().UTC()
	if err != nil {
		s.tracker.recordError(now, err)
		if s.logger != nil {
			s.logger.Error("audit write failed",
				observability.KeyBundleID, event.BundleID,
				observability.KeyRequestID, event.RequestID,
				"kind", event.Kind,
				observability.KeyError, err.Error())
		}
		return err
	}
	s.tracker.recordSuccess(now)
	return nil
}
