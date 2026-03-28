package govern

import "strings"

const (
	TracePhaseGovernance = "governance"
	TracePhaseMatch      = "match"
	TracePhaseEffect     = "effect"
)

const (
	TraceScopeRule              = "rule"
	TraceScopeFlag              = "flag"
	TraceScopeFlagRule          = "flag_rule"
	TraceScopeStrategyCandidate = "strategy_candidate"
	TraceScopeExpertRule        = "expert_rule"
)

const (
	TraceKindKillSwitch = "kill_switch"
	TraceKindRequires   = "requires"
	TraceKindExcludes   = "excludes"
	TraceKindSegment    = "segment"
	TraceKindCondition  = "condition"
	TraceKindRollout    = "rollout"
	TraceKindSplit      = "split"
	TraceKindFallback   = "fallback"
	TraceKindCycle      = "cycle"
)

// Trace records evaluation decisions. It is not goroutine-safe.
type Trace struct {
	Steps []TraceStep `json:"steps,omitempty"`
}

// TraceStep records one governance check.
type TraceStep struct {
	Check   string `json:"check"`
	Result  bool   `json:"result"`
	Detail  string `json:"detail"`
	Phase   string `json:"phase,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Subject string `json:"subject,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Target  string `json:"target,omitempty"`
}

// Append adds a trace step. It is a no-op on a nil receiver.
func (t *Trace) Append(check string, result bool, detail string) {
	t.AppendStep(NewTraceStep(check, result, detail))
}

// AppendScoped adds a trace step with normalized semantic metadata.
func (t *Trace) AppendScoped(phase, scope, subject, kind, target, check string, result bool, detail string) {
	t.AppendStep(NewScopedTraceStep(phase, scope, subject, kind, target, check, result, detail))
}

// AppendStep adds one trace step, normalizing semantic metadata from the check
// label when structured fields were not set explicitly.
func (t *Trace) AppendStep(step TraceStep) {
	if t == nil {
		return
	}
	step.normalize()
	t.Steps = append(t.Steps, step)
}

// NewTraceStep builds one trace step and infers structured semantics from the
// legacy check label.
func NewTraceStep(check string, result bool, detail string) TraceStep {
	step := TraceStep{
		Check:  check,
		Result: result,
		Detail: detail,
	}
	step.normalize()
	return step
}

// NewScopedTraceStep builds one structured trace step while preserving the
// legacy check label for existing clients.
func NewScopedTraceStep(phase, scope, subject, kind, target, check string, result bool, detail string) TraceStep {
	step := TraceStep{
		Check:   check,
		Result:  result,
		Detail:  detail,
		Phase:   phase,
		Scope:   scope,
		Subject: subject,
		Kind:    kind,
		Target:  target,
	}
	step.normalize()
	return step
}

func (s *TraceStep) normalize() {
	if s == nil {
		return
	}
	if s.Check == "" {
		s.Check = defaultTraceCheck(s.Kind, s.Target)
	}
	if s.Check == "" {
		return
	}
	if s.Scope == "" || s.Subject == "" || s.Kind == "" || s.Target == "" {
		inferTraceSemantics(s)
	}
	if s.Phase == "" {
		s.Phase = tracePhaseForKind(s.Kind)
	}
	if s.Check == "" {
		s.Check = defaultTraceCheck(s.Kind, s.Target)
	}
}

func inferTraceSemantics(step *TraceStep) {
	check := strings.TrimSpace(step.Check)
	if check == "" {
		return
	}
	if strings.HasPrefix(check, "strategy:") {
		rest := strings.TrimPrefix(check, "strategy:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) == 2 {
			if step.Scope == "" {
				step.Scope = TraceScopeStrategyCandidate
			}
			if step.Subject == "" {
				step.Subject = parts[0]
			}
			if step.Kind == "" {
				step.Kind = parts[1]
			}
		}
		return
	}
	switch {
	case check == "kill_switch":
		if step.Kind == "" {
			step.Kind = TraceKindKillSwitch
		}
	case check == "cycle detection":
		if step.Kind == "" {
			step.Kind = TraceKindCycle
		}
	case check == "inline condition" || check == "condition":
		if step.Kind == "" {
			step.Kind = TraceKindCondition
		}
	case check == "fallback":
		if step.Kind == "" {
			step.Kind = TraceKindFallback
		}
	case strings.HasPrefix(check, "requires "):
		if step.Kind == "" {
			step.Kind = TraceKindRequires
		}
		if step.Target == "" {
			step.Target = strings.TrimPrefix(check, "requires ")
		}
	case strings.HasPrefix(check, "excludes "):
		if step.Kind == "" {
			step.Kind = TraceKindExcludes
		}
		if step.Target == "" {
			step.Target = strings.TrimPrefix(check, "excludes ")
		}
	case strings.HasPrefix(check, "segment "):
		if step.Kind == "" {
			step.Kind = TraceKindSegment
		}
		if step.Target == "" {
			step.Target = strings.TrimPrefix(check, "segment ")
		}
	case strings.HasPrefix(check, "rollout percent "):
		if step.Kind == "" {
			step.Kind = TraceKindRollout
		}
	case strings.HasPrefix(check, "split by "):
		if step.Kind == "" {
			step.Kind = TraceKindSplit
		}
	}
}

func tracePhaseForKind(kind string) string {
	switch kind {
	case TraceKindKillSwitch, TraceKindRequires, TraceKindExcludes, TraceKindRollout, TraceKindCycle:
		return TracePhaseGovernance
	case TraceKindSegment, TraceKindCondition, TraceKindFallback:
		return TracePhaseMatch
	case TraceKindSplit:
		return TracePhaseEffect
	default:
		return ""
	}
}

func defaultTraceCheck(kind, target string) string {
	switch kind {
	case TraceKindKillSwitch:
		return "kill_switch"
	case TraceKindRequires:
		if target != "" {
			return "requires " + target
		}
	case TraceKindExcludes:
		if target != "" {
			return "excludes " + target
		}
	case TraceKindSegment:
		if target != "" {
			return "segment " + target
		}
	case TraceKindCondition:
		return "condition"
	case TraceKindRollout:
		if target != "" {
			return target
		}
		return "rollout"
	case TraceKindSplit:
		if target != "" {
			return target
		}
		return "split"
	case TraceKindFallback:
		return "fallback"
	case TraceKindCycle:
		return "cycle detection"
	}
	return ""
}
