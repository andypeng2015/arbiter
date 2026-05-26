package audit

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"m31labs.dev/arbiter/govern"
)

// RuleMatch captures one matched rule for durable auditing.
type RuleMatch struct {
	Name     string         `json:"name"`
	Priority int            `json:"priority"`
	Action   string         `json:"action"`
	Params   map[string]any `json:"params,omitempty"`
	Fallback bool           `json:"fallback"`
}

// FlagDecision captures one resolved flag for durable auditing.
type FlagDecision struct {
	Flag        string         `json:"flag"`
	Variant     string         `json:"variant"`
	Values      map[string]any `json:"values,omitempty"`
	IsDefault   bool           `json:"is_default"`
	Reason      string         `json:"reason,omitempty"`
	Environment string         `json:"environment,omitempty"`
}

// FlagAssignment is emitted for every non-default flag resolution.
// Designed for experimentation pipelines: join on UserID in your
// analytics warehouse to compute variant lift and significance.
type FlagAssignment struct {
	Flag        string         `json:"flag"`
	Variant     string         `json:"variant"`
	UserID      string         `json:"user_id,omitempty"`
	Environment string         `json:"environment,omitempty"`
	Values      map[string]any `json:"values,omitempty"`
}

// StrategyDecision captures one resolved strategy selection.
type StrategyDecision struct {
	Strategy string         `json:"strategy"`
	Outcome  string         `json:"outcome"`
	Selected string         `json:"selected"`
	Params   map[string]any `json:"params,omitempty"`
}

// ExpertFact captures one expert working-memory fact.
type ExpertFact struct {
	Type      string         `json:"type"`
	Key       string         `json:"key"`
	Fields    map[string]any `json:"fields,omitempty"`
	DerivedBy []string       `json:"derived_by,omitempty"`
}

// ExpertOutcome captures one emitted expert outcome.
type ExpertOutcome struct {
	Rule   string         `json:"rule"`
	Name   string         `json:"name"`
	Params map[string]any `json:"params,omitempty"`
}

// ExpertActivation captures one expert activation.
type ExpertActivation struct {
	Round     int                    `json:"round"`
	Rule      string                 `json:"rule"`
	Kind      string                 `json:"kind"`
	Target    string                 `json:"target"`
	Params    map[string]any         `json:"params,omitempty"`
	Changed   bool                   `json:"changed"`
	Detail    string                 `json:"detail,omitempty"`
	Arbitrace []govern.ArbitraceStep `json:"arbitrace,omitempty"`
}

// ExpertDecision captures the result of an expert session run.
type ExpertDecision struct {
	SessionID       string             `json:"session_id"`
	Outcomes        []ExpertOutcome    `json:"outcomes,omitempty"`
	Facts           []ExpertFact       `json:"facts,omitempty"`
	Activations     []ExpertActivation `json:"activations,omitempty"`
	StopReason      string             `json:"stop_reason,omitempty"`
	Rounds          int                `json:"rounds,omitempty"`
	Mutations       int                `json:"mutations,omitempty"`
	StableDeferred  bool               `json:"stable_deferred,omitempty"`
	TemporalPending bool               `json:"temporal_pending,omitempty"`
}

// OverrideChange captures one runtime override mutation.
type OverrideChange struct {
	Scope           string  `json:"scope"`
	Target          string  `json:"target"`
	RuleIndex       *int    `json:"rule_index,omitempty"`
	KillSwitch      *bool   `json:"kill_switch,omitempty"`
	KillSwitchState string  `json:"kill_switch_state,omitempty"`
	Rollout         *uint16 `json:"rollout,omitempty"`
}

// BundleChange captures one bundle publish or activation mutation.
type BundleChange struct {
	Action           string `json:"action"`
	Name             string `json:"name"`
	BundleID         string `json:"bundle_id"`
	Checksum         string `json:"checksum,omitempty"`
	PreviousBundleID string `json:"previous_bundle_id,omitempty"`
}

// DecisionEvent is the durable audit record for one governance request.
type DecisionEvent struct {
	Timestamp   time.Time              `json:"timestamp"`
	RequestID   string                 `json:"request_id,omitempty"`
	BundleID    string                 `json:"bundle_id"`
	Environment string                 `json:"environment,omitempty"`
	Kind        string                 `json:"kind"`
	Context     map[string]any         `json:"context,omitempty"`
	Rules       []RuleMatch            `json:"rules,omitempty"`
	Flag        *FlagDecision          `json:"flag,omitempty"`
	Assignment  *FlagAssignment        `json:"assignment,omitempty"`
	Strategy    *StrategyDecision      `json:"strategy,omitempty"`
	Expert      *ExpertDecision        `json:"expert,omitempty"`
	Override    *OverrideChange        `json:"override,omitempty"`
	Bundle      *BundleChange          `json:"bundle,omitempty"`
	Arbitrace   []govern.ArbitraceStep `json:"arbitrace,omitempty"`
}

// Sink persists governance decisions.
type Sink interface {
	WriteDecision(ctx context.Context, event DecisionEvent) error
}

// NopSink discards all events.
type NopSink struct{}

// WriteDecision implements Sink.
func (NopSink) WriteDecision(context.Context, DecisionEvent) error { return nil }

// JSONLSink appends one JSON object per line to a file.
type JSONLSink struct {
	mu sync.Mutex
	f  *os.File
}

// NewJSONLSink opens a JSONL audit sink.
func NewJSONLSink(path string) (*JSONLSink, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &JSONLSink{f: f}, nil
}

// WriteDecision implements Sink.
func (s *JSONLSink) WriteDecision(_ context.Context, event DecisionEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	enc := json.NewEncoder(s.f)
	return enc.Encode(event)
}

// Close closes the underlying file.
func (s *JSONLSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}
