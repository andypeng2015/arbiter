package strategy

import (
	"fmt"
	"sort"

	"github.com/odvcencio/arbiter/compiler"
	"github.com/odvcencio/arbiter/govern"
	"github.com/odvcencio/arbiter/ir"
	"github.com/odvcencio/arbiter/overrides"
	"github.com/odvcencio/arbiter/vm"
)

// Strategies is a compiled set of strategy declarations over named decision shapes.
type Strategies struct {
	defs     map[string]*Definition
	segments *govern.SegmentSet
}

// Definition is one executable strategy declaration.
type Definition struct {
	Name       string
	Returns    string
	Ruleset    *compiler.CompiledRuleset
	Candidates []Candidate
}

// Candidate captures one recognizable strategy shape/path within an ordered declaration.
type Candidate struct {
	Label      string
	Segment    string
	Condition  string
	KillSwitch ir.KillSwitchState
	Rollout    *govern.PercentRollout
	IsElse     bool
}

// Result is the outcome of recognizing and selecting one strategy path.
type Result struct {
	Strategy string         `json:"strategy"`
	Outcome  string         `json:"outcome"`
	Selected string         `json:"selected"`
	Params   map[string]any `json:"params,omitempty"`
	Trace    govern.Trace   `json:"trace,omitempty"`
}

// Compile builds executable strategy plans from a lowered program and segment set.
func Compile(program *ir.Program, segments *govern.SegmentSet) (*Strategies, error) {
	if program == nil {
		return nil, fmt.Errorf("nil lowered program")
	}
	if segments == nil {
		segments = govern.NewSegmentSet()
	}
	s := &Strategies{
		defs:     make(map[string]*Definition, len(program.Strategies)),
		segments: segments,
	}
	for i := range program.Strategies {
		def, err := lowerDefinition(program, &program.Strategies[i])
		if err != nil {
			return nil, err
		}
		s.defs[def.Name] = def
	}
	return s, nil
}

// Evaluate recognizes and selects one named strategy path against a request context.
func (s *Strategies) Evaluate(name string, ctx map[string]any) (Result, error) {
	return s.EvaluateWithOverrides(name, ctx, "", nil)
}

// EvaluateWithOverrides recognizes and selects one named strategy path while
// applying runtime candidate overrides.
func (s *Strategies) EvaluateWithOverrides(name string, ctx map[string]any, bundleID string, view overrides.View) (Result, error) {
	if s == nil {
		return Result{}, fmt.Errorf("nil strategies")
	}
	def, ok := s.defs[name]
	if !ok {
		return Result{}, fmt.Errorf("strategy %q not found", name)
	}
	if ctx == nil {
		ctx = map[string]any{}
	}

	sp := vm.NewStringPool(def.Ruleset.Constants.Strings())
	dc := vm.DataFromMap(ctx, sp)
	evaluator := vm.NewEvaluator(def.Ruleset, sp)
	rc := govern.NewRequestCache(s.segments, ctx)
	trace := &govern.Trace{}

	for i, rule := range def.Ruleset.Rules {
		candidate := def.Candidates[i]
		subject := def.Name + "/" + candidate.Label
		checkPrefix := "strategy:" + def.Name + "/" + candidate.Label + ":"
		killSwitch, rollout := effectiveCandidateGovernance(bundleID, def.Name, candidate, view)

		if killSwitch.RecordScoped(trace, govern.TraceScopeStrategyCandidate, subject, checkPrefix+"kill_switch") {
			continue
		}

		if candidate.Segment != "" {
			ok, detail := rc.EvalSegment(candidate.Segment)
			trace.AppendScoped(govern.TracePhaseMatch, govern.TraceScopeStrategyCandidate, subject, govern.TraceKindSegment, candidate.Segment, checkPrefix+"segment", ok, detail)
			if !ok {
				continue
			}
		}

		matched, err := evaluator.EvalRuleCondition(rule, dc)
		if err != nil {
			return Result{}, fmt.Errorf("strategy %s candidate %s: %w", def.Name, candidate.Label, err)
		}
		if candidate.IsElse {
			trace.AppendScoped(govern.TracePhaseMatch, govern.TraceScopeStrategyCandidate, subject, govern.TraceKindFallback, "", checkPrefix+"fallback", matched, "else arm selected")
		} else {
			trace.AppendScoped(govern.TracePhaseMatch, govern.TraceScopeStrategyCandidate, subject, govern.TraceKindCondition, "", checkPrefix+"condition", matched, candidate.Condition)
		}
		if !matched {
			continue
		}

		if rollout != nil {
			decision := govern.DecidePercentRollout(*rollout, rc.Context())
			trace.AppendScoped(govern.TracePhaseGovernance, govern.TraceScopeStrategyCandidate, subject, govern.TraceKindRollout, rollout.Namespace, checkPrefix+"rollout", decision.Allowed, decision.Detail())
			if !decision.Allowed {
				continue
			}
		}

		outcome, err := evaluator.BuildMatch(rule, dc)
		if err != nil {
			return Result{}, fmt.Errorf("strategy %s candidate %s: %w", def.Name, candidate.Label, err)
		}
		return Result{
			Strategy: def.Name,
			Outcome:  def.Returns,
			Selected: candidate.Label,
			Params:   outcome.Params,
			Trace: govern.Trace{
				Steps: append([]govern.TraceStep(nil), trace.Steps...),
			},
		}, nil
	}

	return Result{}, fmt.Errorf("strategy %s: no candidate selected", def.Name)
}

// Count returns the number of loaded strategies.
func (s *Strategies) Count() int {
	if s == nil {
		return 0
	}
	return len(s.defs)
}

// Has reports whether a strategy with the given name exists.
func (s *Strategies) Has(name string) bool {
	if s == nil {
		return false
	}
	_, ok := s.defs[name]
	return ok
}

// HasCandidate reports whether one strategy contains the named candidate label.
func (s *Strategies) HasCandidate(name, label string) bool {
	if s == nil {
		return false
	}
	def, ok := s.defs[name]
	if !ok {
		return false
	}
	for _, candidate := range def.Candidates {
		if candidate.Label == label {
			return true
		}
	}
	return false
}

// Names returns all strategy names in sorted order.
func (s *Strategies) Names() []string {
	if s == nil {
		return nil
	}
	names := make([]string, 0, len(s.defs))
	for name := range s.defs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func lowerDefinition(program *ir.Program, decl *ir.Strategy) (*Definition, error) {
	if program == nil || decl == nil {
		return nil, fmt.Errorf("nil strategy declaration")
	}
	rs, err := compileStrategyRuleset(program, decl)
	if err != nil {
		return nil, fmt.Errorf("strategy %s: %w", decl.Name, err)
	}
	def := &Definition{
		Name:    decl.Name,
		Returns: decl.Returns,
		Ruleset: rs,
	}
	for i := range decl.Candidates {
		candidate := decl.Candidates[i]
		item := Candidate{
			Label:      candidate.Label,
			Segment:    candidate.Segment,
			KillSwitch: candidate.KillSwitch,
			IsElse:     candidate.IsElse,
		}
		if candidate.HasCondition {
			item.Condition = ir.RenderExpr(program, candidate.Condition)
		}
		if candidate.Rollout != nil {
			item.Rollout = buildCandidateRollout(decl.Name, &candidate)
		}
		def.Candidates = append(def.Candidates, item)
	}
	return def, nil
}

func compileStrategyRuleset(program *ir.Program, decl *ir.Strategy) (*compiler.CompiledRuleset, error) {
	synthetic := &ir.Program{
		Consts: program.Consts,
		Exprs:  append([]ir.Expr(nil), program.Exprs...),
	}
	trueID := ir.ExprID(len(synthetic.Exprs))
	synthetic.Exprs = append(synthetic.Exprs, ir.Expr{
		Kind: ir.ExprBoolLit,
		Bool: true,
	})

	for i := range decl.Candidates {
		candidate := decl.Candidates[i]
		rule := ir.Rule{
			Name:       candidate.Label,
			KillSwitch: candidate.KillSwitch,
			Segment:    candidate.Segment,
			Lets:       append([]ir.LetBinding(nil), candidate.Lets...),
			Rollout:    cloneRollout(candidate.Rollout),
			Action: ir.Action{
				Name:   candidate.Label,
				Params: append([]ir.ActionParam(nil), candidate.Params...),
			},
		}
		if candidate.IsElse {
			rule.Condition = trueID
			rule.HasCondition = true
		} else {
			rule.Condition = candidate.Condition
			rule.HasCondition = candidate.HasCondition
		}
		synthetic.Rules = append(synthetic.Rules, rule)
	}

	synthetic.RebuildIndexes()
	rs, err := compiler.CompileIR(synthetic)
	if err != nil {
		return nil, err
	}
	return rs, nil
}

func buildCandidateRollout(strategyName string, candidate *ir.StrategyCandidate) *govern.PercentRollout {
	if candidate == nil || candidate.Rollout == nil {
		return nil
	}
	subject := govern.DefaultRolloutSubject
	if candidate.Rollout.HasSubject {
		subject = candidate.Rollout.Subject
	}
	namespace := candidate.Rollout.Namespace
	if !candidate.Rollout.HasNamespace {
		namespace = govern.AutoRolloutNamespace("", "strategy:"+strategyName+":candidate:"+candidate.Label)
	}
	return &govern.PercentRollout{
		PercentBps: candidate.Rollout.Bps,
		SubjectKey: subject,
		Namespace:  namespace,
	}
}

func cloneRollout(rollout *ir.Rollout) *ir.Rollout {
	if rollout == nil {
		return nil
	}
	clone := *rollout
	return &clone
}

func effectiveCandidateGovernance(bundleID, strategyName string, candidate Candidate, view overrides.View) (govern.KillSwitchDecision, *govern.PercentRollout) {
	rollout := candidate.Rollout
	var override *bool
	if view == nil {
		return govern.ResolveKillSwitch(candidate.KillSwitch.IsSet(), candidate.KillSwitch.Enabled(), nil), rollout
	}
	ov, ok := view.Strategy(bundleID, strategyName, candidate.Label)
	if !ok {
		return govern.ResolveKillSwitch(candidate.KillSwitch.IsSet(), candidate.KillSwitch.Enabled(), nil), rollout
	}
	if ov.KillSwitch != nil {
		override = ov.KillSwitch
	}
	if ov.Rollout != nil {
		rollout = overrideCandidateRollout(bundleID, strategyName, candidate, *ov.Rollout)
	}
	return govern.ResolveKillSwitch(candidate.KillSwitch.IsSet(), candidate.KillSwitch.Enabled(), override), rollout
}

func overrideCandidateRollout(bundleID, strategyName string, candidate Candidate, rolloutBps uint16) *govern.PercentRollout {
	if candidate.Rollout != nil {
		spec := *candidate.Rollout
		spec.PercentBps = rolloutBps
		return &spec
	}
	return &govern.PercentRollout{
		PercentBps: rolloutBps,
		SubjectKey: govern.DefaultRolloutSubject,
		Namespace:  govern.AutoRolloutNamespace(bundleID, "strategy:"+strategyName+":candidate:"+candidate.Label),
	}
}
