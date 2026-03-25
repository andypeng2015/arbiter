package workflow

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/expert"
	"github.com/odvcencio/arbiter/expert/factsource"
)

func (r *Runner) syncSources(ctx context.Context) error {
	targets := r.workflow.ExternalSources()
	slices.Sort(targets)
	for _, target := range targets {
		if err := r.syncSourceTarget(ctx, target); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) syncSourceTarget(ctx context.Context, target string) error {
	state := r.sourceState(target)
	if state == nil {
		return nil
	}

	now := r.now().UTC()
	r.mu.RLock()
	retryPending := sourceRetryPending(state, now)
	lastFacts := cloneExpertFacts(state.lastFacts)
	r.mu.RUnlock()
	if retryPending {
		return r.restoreSourceFacts(target, lastFacts)
	}

	facts, err := r.loadSourceWithRetry(ctx, target)
	if err != nil {
		r.mu.RLock()
		lastFacts = cloneExpertFacts(state.lastFacts)
		r.mu.RUnlock()
		return r.restoreSourceFacts(target, lastFacts)
	}

	r.mu.Lock()
	r.markSourceLoadSuccess(state, facts, now)
	r.mu.Unlock()
	return r.workflow.SetSourceFacts(target, facts)
}

func sourceRetryPending(state *sourceState, now time.Time) bool {
	return !state.NextRetryAt.IsZero() && now.Before(state.NextRetryAt)
}

func (r *Runner) restoreSourceFacts(target string, facts []expert.Fact) error {
	if len(facts) == 0 {
		return r.workflow.SetSourceFacts(target, nil)
	}
	return r.workflow.SetSourceFacts(target, facts)
}

func (r *Runner) markSourceLoadSuccess(state *sourceState, facts []expert.Fact, now time.Time) {
	state.Available = true
	state.LastError = ""
	state.ConsecutiveFailures = 0
	state.NextRetryAt = time.Time{}
	state.LastSuccessAt = now
	state.FactCount = len(facts)
	state.lastFacts = cloneExpertFacts(facts)
}

func (r *Runner) loadSourceWithRetry(ctx context.Context, target string) ([]expert.Fact, error) {
	state := r.sourceState(target)
	if state == nil {
		return nil, fmt.Errorf("nil source state")
	}

	backoff := r.initialBackoff
	var lastErr error
	for attempt := 1; attempt <= r.sourceAttempts; attempt++ {
		now := r.now().UTC()
		r.mu.Lock()
		state.LastAttemptAt = now
		r.mu.Unlock()
		facts, err := r.loader(ctx, state.Target)
		if err == nil {
			return facts, nil
		}
		lastErr = err
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if attempt == r.sourceAttempts {
			break
		}
		if err := sleepContext(ctx, backoff); err != nil {
			return nil, err
		}
		backoff = nextBackoff(backoff, r.maxBackoff)
	}

	r.mu.Lock()
	state.Available = false
	state.LastError = lastErr.Error()
	state.ConsecutiveFailures++
	state.NextRetryAt = r.now().UTC().Add(backoff)
	state.FactCount = len(state.lastFacts)
	r.mu.Unlock()
	return nil, lastErr
}

func (r *Runner) syncArbiterEnvelopes() {
	r.mu.RLock()
	sources := r.sourceStatesLocked()
	sinks := r.sinkStatesLocked()
	r.mu.RUnlock()
	now := r.now().UTC()
	for _, name := range r.workflow.order {
		arb := r.workflow.arbiters[name]
		if arb == nil || arb.session == nil {
			continue
		}
		envelope := cloneMap(arb.baseEnvelope)
		if envelope == nil {
			envelope = make(map[string]any)
		}
		if src := sourceEnvelope(arb, sources, now); len(src) > 0 {
			envelope["source"] = src
		}
		if sink := sinkEnvelope(arb, sinks); len(sink) > 0 {
			envelope["sink"] = sink
		}
		arb.session.SetEnvelope(envelope)
	}
}

func sourceEnvelope(arb *runtimeArbiter, states map[string]SourceSnapshot, now time.Time) map[string]any {
	if arb == nil {
		return nil
	}
	out := make(map[string]any)
	for _, source := range arb.decl.Sources {
		if isChainSourceTarget(source.Target) {
			continue
		}
		state, ok := states[source.Target]
		if !ok {
			continue
		}
		out[state.Alias] = sourceEnvelopeMeta(state, now)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sinkEnvelope(arb *runtimeArbiter, states map[string]SinkSnapshot) map[string]any {
	if arb == nil {
		return nil
	}
	out := make(map[string]any)
	for _, handler := range arb.decl.Handlers {
		if handler.Kind == arbiter.ArbiterHandlerChain {
			continue
		}
		state, ok := states[sinkHandlerKey(handler)]
		if !ok {
			continue
		}
		out[state.Alias] = sinkEnvelopeMeta(state)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sourceEnvelopeMeta(state SourceSnapshot, now time.Time) map[string]any {
	meta := map[string]any{
		"target":               state.Target,
		"alias":                state.Alias,
		"available":            state.Available,
		"fact_count":           float64(state.FactCount),
		"consecutive_failures": float64(state.ConsecutiveFailures),
	}
	if state.LastError != "" {
		meta["last_error"] = state.LastError
	}
	if !state.LastAttemptAt.IsZero() {
		meta["last_attempt_at"] = float64(state.LastAttemptAt.Unix())
	}
	if !state.LastSuccessAt.IsZero() {
		meta["last_success_at"] = float64(state.LastSuccessAt.Unix())
	}
	if !state.NextRetryAt.IsZero() {
		meta["next_retry_at"] = float64(state.NextRetryAt.Unix())
	}
	meta["__source_age_seconds"] = sourceAgeSeconds(state, now)
	return meta
}

func sourceAgeSeconds(state SourceSnapshot, now time.Time) float64 {
	if state.LastSuccessAt.IsZero() || now.Before(state.LastSuccessAt) {
		return 0
	}
	return float64(int64(now.Sub(state.LastSuccessAt).Seconds()))
}

func sinkEnvelopeMeta(state SinkSnapshot) map[string]any {
	meta := map[string]any{
		"kind":                 state.Kind,
		"target":               state.Target,
		"available":            state.Available,
		"pending":              float64(state.Pending),
		"consecutive_failures": float64(state.ConsecutiveFailures),
	}
	if state.LastError != "" {
		meta["last_error"] = state.LastError
	}
	if !state.LastAttemptAt.IsZero() {
		meta["last_attempt_at"] = float64(state.LastAttemptAt.Unix())
	}
	if !state.LastSuccessAt.IsZero() {
		meta["last_success_at"] = float64(state.LastSuccessAt.Unix())
	}
	if !state.NextRetryAt.IsZero() {
		meta["next_retry_at"] = float64(state.NextRetryAt.Unix())
	}
	return meta
}

func (r *Runner) sourceState(target string) *sourceState {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sources[target]
}

func toExpertFacts(facts []factsource.Fact) []expert.Fact {
	out := make([]expert.Fact, 0, len(facts))
	for _, fact := range facts {
		out = append(out, expert.Fact{
			Type:   fact.Type,
			Key:    fact.Key,
			Fields: cloneMap(fact.Fields),
		})
	}
	return out
}

func runtimeAlias(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "runtime"
	}
	if parsed := strings.TrimPrefix(raw, "chain://"); parsed != raw {
		raw = parsed
	}
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	alias := strings.Trim(b.String(), "_")
	if alias == "" {
		return "runtime"
	}
	if alias[0] >= '0' && alias[0] <= '9' {
		return "runtime_" + alias
	}
	return alias
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
