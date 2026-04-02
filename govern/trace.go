package govern

import "strings"

const (
	ArbitracePhaseGovernance = "governance"
	ArbitracePhaseMatch      = "match"
	ArbitracePhaseEffect     = "effect"
)

const (
	ArbitraceScopeRule              = "rule"
	ArbitraceScopeFlag              = "flag"
	ArbitraceScopeFlagRule          = "flag_rule"
	ArbitraceScopeStrategyCandidate = "strategy_candidate"
	ArbitraceScopeExpertRule        = "expert_rule"
)

const (
	ArbitraceKindKillSwitch  = "kill_switch"
	ArbitraceKindActiveFrom  = "active_from"
	ArbitraceKindActiveUntil = "active_until"
	ArbitraceKindRequires    = "requires"
	ArbitraceKindExcludes    = "excludes"
	ArbitraceKindSegment     = "segment"
	ArbitraceKindCondition   = "condition"
	ArbitraceKindRollout     = "rollout"
	ArbitraceKindSplit       = "split"
	ArbitraceKindFallback    = "fallback"
	ArbitraceKindCycle       = "cycle"
)

const (
	ArbitraceDispositionPassed   = "passed"
	ArbitraceDispositionBlocked  = "blocked"
	ArbitraceDispositionDeferred = "deferred"
)

// Arbitrace records evaluation decisions. It is not goroutine-safe.
type Arbitrace struct {
	Steps []ArbitraceStep `json:"steps,omitempty"`
}

// ArbitraceStep records one governance check.
type ArbitraceStep struct {
	Check   string `json:"check"`
	Result  bool   `json:"result"`
	Detail  string `json:"detail"`
	Phase   string `json:"phase,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Subject string `json:"subject,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Target  string `json:"target,omitempty"`
	// Disposition is Arbiter's canonical tri-state outcome for this step.
	// `result` remains the legacy bool surface; deferred steps set result=false.
	Disposition string `json:"disposition,omitempty"`
}

// Clone returns a copy of the arbitrace and its steps.
func (a Arbitrace) Clone() Arbitrace {
	if len(a.Steps) == 0 {
		return Arbitrace{}
	}
	clone := Arbitrace{Steps: make([]ArbitraceStep, len(a.Steps))}
	copy(clone.Steps, a.Steps)
	return clone
}

// Append adds an arbitrace step. It is a no-op on a nil receiver.
func (a *Arbitrace) Append(check string, result bool, detail string) {
	a.AppendStep(NewArbitraceStep(check, result, detail))
}

// AppendScoped adds an arbitrace step with normalized semantic metadata.
func (a *Arbitrace) AppendScoped(phase, scope, subject, kind, target, check string, result bool, detail string) {
	a.AppendStep(NewScopedArbitraceStep(phase, scope, subject, kind, target, check, result, detail))
}

// AppendDeferredScoped adds one explicitly deferred arbitrace step.
func (a *Arbitrace) AppendDeferredScoped(phase, scope, subject, kind, target, check string, detail string) {
	a.AppendStep(NewDeferredScopedArbitraceStep(phase, scope, subject, kind, target, check, detail))
}

// AppendStep adds one arbitrace step, normalizing semantic metadata from the check
// label when structured fields were not set explicitly.
func (a *Arbitrace) AppendStep(step ArbitraceStep) {
	if a == nil {
		return
	}
	step.normalize()
	a.Steps = append(a.Steps, step)
}

// NewArbitraceStep builds one arbitrace step and infers structured semantics from the
// legacy check label.
func NewArbitraceStep(check string, result bool, detail string) ArbitraceStep {
	step := ArbitraceStep{
		Check:  check,
		Result: result,
		Detail: detail,
	}
	step.normalize()
	return step
}

// NewScopedArbitraceStep builds one structured arbitrace step while preserving the
// legacy check label for existing clients.
func NewScopedArbitraceStep(phase, scope, subject, kind, target, check string, result bool, detail string) ArbitraceStep {
	step := ArbitraceStep{
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

// NewDeferredScopedArbitraceStep builds one structured deferred arbitrace step.
func NewDeferredScopedArbitraceStep(phase, scope, subject, kind, target, check string, detail string) ArbitraceStep {
	step := NewScopedArbitraceStep(phase, scope, subject, kind, target, check, false, detail)
	step.Disposition = ArbitraceDispositionDeferred
	step.normalize()
	return step
}

func (s *ArbitraceStep) normalize() {
	if s == nil {
		return
	}
	if s.Check == "" {
		s.Check = defaultArbitraceCheck(s.Kind, s.Target)
	}
	if s.Check == "" {
		return
	}
	if s.Scope == "" || s.Subject == "" || s.Kind == "" || s.Target == "" {
		inferArbitraceSemantics(s)
	}
	if s.Phase == "" {
		s.Phase = arbitracePhaseForKind(s.Kind)
	}
	if s.Check == "" {
		s.Check = defaultArbitraceCheck(s.Kind, s.Target)
	}
	if s.Disposition == "" {
		if s.Result {
			s.Disposition = ArbitraceDispositionPassed
		} else {
			s.Disposition = ArbitraceDispositionBlocked
		}
	}
}

func inferArbitraceSemantics(step *ArbitraceStep) {
	check := strings.TrimSpace(step.Check)
	if check == "" {
		return
	}
	if strings.HasPrefix(check, "strategy:") {
		rest := strings.TrimPrefix(check, "strategy:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) == 2 {
			if step.Scope == "" {
				step.Scope = ArbitraceScopeStrategyCandidate
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
			step.Kind = ArbitraceKindKillSwitch
		}
	case check == "active_from":
		if step.Kind == "" {
			step.Kind = ArbitraceKindActiveFrom
		}
	case check == "active_until":
		if step.Kind == "" {
			step.Kind = ArbitraceKindActiveUntil
		}
	case check == "cycle detection":
		if step.Kind == "" {
			step.Kind = ArbitraceKindCycle
		}
	case check == "inline condition" || check == "condition":
		if step.Kind == "" {
			step.Kind = ArbitraceKindCondition
		}
	case check == "fallback":
		if step.Kind == "" {
			step.Kind = ArbitraceKindFallback
		}
	case strings.HasPrefix(check, "requires "):
		if step.Kind == "" {
			step.Kind = ArbitraceKindRequires
		}
		if step.Target == "" {
			step.Target = strings.TrimPrefix(check, "requires ")
		}
	case strings.HasPrefix(check, "active_from "):
		if step.Kind == "" {
			step.Kind = ArbitraceKindActiveFrom
		}
		if step.Target == "" {
			step.Target = strings.TrimPrefix(check, "active_from ")
		}
	case strings.HasPrefix(check, "active_until "):
		if step.Kind == "" {
			step.Kind = ArbitraceKindActiveUntil
		}
		if step.Target == "" {
			step.Target = strings.TrimPrefix(check, "active_until ")
		}
	case strings.HasPrefix(check, "excludes "):
		if step.Kind == "" {
			step.Kind = ArbitraceKindExcludes
		}
		if step.Target == "" {
			step.Target = strings.TrimPrefix(check, "excludes ")
		}
	case strings.HasPrefix(check, "segment "):
		if step.Kind == "" {
			step.Kind = ArbitraceKindSegment
		}
		if step.Target == "" {
			step.Target = strings.TrimPrefix(check, "segment ")
		}
	case strings.HasPrefix(check, "rollout percent "):
		if step.Kind == "" {
			step.Kind = ArbitraceKindRollout
		}
	case strings.HasPrefix(check, "split by "):
		if step.Kind == "" {
			step.Kind = ArbitraceKindSplit
		}
	}
}

func arbitracePhaseForKind(kind string) string {
	switch kind {
	case ArbitraceKindKillSwitch, ArbitraceKindActiveFrom, ArbitraceKindActiveUntil, ArbitraceKindRequires, ArbitraceKindExcludes, ArbitraceKindRollout, ArbitraceKindCycle:
		return ArbitracePhaseGovernance
	case ArbitraceKindSegment, ArbitraceKindCondition, ArbitraceKindFallback:
		return ArbitracePhaseMatch
	case ArbitraceKindSplit:
		return ArbitracePhaseEffect
	default:
		return ""
	}
}

func defaultArbitraceCheck(kind, target string) string {
	switch kind {
	case ArbitraceKindKillSwitch:
		return "kill_switch"
	case ArbitraceKindActiveFrom:
		if target != "" {
			return "active_from " + target
		}
		return "active_from"
	case ArbitraceKindActiveUntil:
		if target != "" {
			return "active_until " + target
		}
		return "active_until"
	case ArbitraceKindRequires:
		if target != "" {
			return "requires " + target
		}
	case ArbitraceKindExcludes:
		if target != "" {
			return "excludes " + target
		}
	case ArbitraceKindSegment:
		if target != "" {
			return "segment " + target
		}
	case ArbitraceKindCondition:
		return "condition"
	case ArbitraceKindRollout:
		if target != "" {
			return target
		}
		return "rollout"
	case ArbitraceKindSplit:
		if target != "" {
			return target
		}
		return "split"
	case ArbitraceKindFallback:
		return "fallback"
	case ArbitraceKindCycle:
		return "cycle detection"
	}
	return ""
}
