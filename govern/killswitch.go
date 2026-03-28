package govern

// KillSwitchDecision preserves both explicit declaration state and the
// effective enabled/disabled outcome after overrides are applied.
type KillSwitchDecision struct {
	Explicit bool
	Enabled  bool
	Detail   string
}

// ResolveKillSwitch combines declaration state with an optional runtime
// override so explainability surfaces can distinguish absent, on, and off.
func ResolveKillSwitch(declared, declaredOn bool, override *bool) KillSwitchDecision {
	if override != nil {
		detail := "kill_switch override enabled"
		if !*override {
			detail = "kill_switch override disabled"
		}
		if declared {
			state := "off"
			if declaredOn {
				state = "on"
			}
			detail += " (declared " + state + ")"
		}
		return KillSwitchDecision{
			Explicit: true,
			Enabled:  *override,
			Detail:   detail,
		}
	}
	if !declared {
		return KillSwitchDecision{}
	}
	detail := "kill_switch declared off"
	if declaredOn {
		detail = "kill_switch declared on"
	}
	return KillSwitchDecision{
		Explicit: true,
		Enabled:  declaredOn,
		Detail:   detail,
	}
}

// Record appends the effective kill-switch decision to the trace and reports
// whether evaluation should be skipped.
func (d KillSwitchDecision) Record(trace *Trace, check string) bool {
	if d.Enabled {
		trace.Append(check, true, d.Detail)
		return true
	}
	if d.Explicit {
		trace.Append(check, false, d.Detail)
	}
	return false
}

// IsKillSwitched reports whether evaluation should be skipped.
func IsKillSwitched(enabled bool, trace *Trace) bool {
	if !enabled {
		return false
	}
	trace.Append("kill_switch", true, "outcome is kill-switched")
	return true
}
