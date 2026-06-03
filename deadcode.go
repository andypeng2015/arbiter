package arbiter

import (
	"fmt"

	"m31labs.dev/arbiter/ir"
)

// detectDeadCode emits non-fatal warnings for logic that can never fire. It
// runs after constant folding, so an always-false condition has already been
// reduced to a boolean literal. Findings are warnings (not errors): they flag
// likely mistakes without rejecting otherwise-valid programs.
func detectDeadCode(program *ir.Program) []Diagnostic {
	if program == nil {
		return nil
	}
	var out []Diagnostic
	warn := func(span ir.Span, msg string) {
		out = append(out, Diagnostic{
			Severity: DiagWarning,
			Message:  msg,
			Line:     int(span.StartRow) + 1,
			Col:      int(span.StartCol) + 1,
		})
	}
	isConstBool := func(id ir.ExprID, want bool) bool {
		e := program.Expr(id)
		return e != nil && e.Kind == ir.ExprBoolLit && e.Bool == want
	}

	for i := range program.Rules {
		r := &program.Rules[i]
		if r.HasCondition && isConstBool(r.Condition, false) {
			warn(r.Span, fmt.Sprintf("rule %s can never match: its condition is always false", r.Name))
		}
	}

	for fi := range program.Flags {
		f := &program.Flags[fi]
		for ri := range f.Rules {
			fr := &f.Rules[ri]
			if fr.HasCondition && isConstBool(fr.Condition, false) {
				warn(fr.Span, fmt.Sprintf("flag %s rule %d can never match: its condition is always false", f.Name, ri))
			}
		}
	}

	for si := range program.Strategies {
		s := &program.Strategies[si]
		unconditionalSeen := false
		for ci := range s.Candidates {
			c := &s.Candidates[ci]
			if unconditionalSeen {
				warn(c.Span, fmt.Sprintf("strategy %s candidate %s is unreachable: an earlier candidate always matches", s.Name, c.Label))
				continue
			}
			if c.HasCondition && isConstBool(c.Condition, false) {
				warn(c.Span, fmt.Sprintf("strategy %s candidate %s can never match: its condition is always false", s.Name, c.Label))
				continue
			}
			// A candidate that always matches (an else, an unconditional arm, or
			// a constant-true condition) shadows everything after it — but only
			// if no rollout / active window / kill switch can make it skip.
			alwaysMatches := c.IsElse ||
				(!c.HasCondition && c.Segment == "") ||
				(c.HasCondition && c.Segment == "" && isConstBool(c.Condition, true))
			if alwaysMatches && c.Rollout == nil && !c.ActiveWindow.Enabled() && c.KillSwitch != ir.KillSwitchOn {
				unconditionalSeen = true
			}
		}
	}

	return out
}
