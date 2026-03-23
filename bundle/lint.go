package bundle

import (
	"fmt"
	"strings"

	"github.com/odvcencio/arbiter/compiler"
)

// Warning is one lint warning about edge-shipping a ruleset.
type Warning struct {
	Severity string // "warn" or "info"
	Message  string
}

// LintForEdge analyzes a compiled ruleset and returns warnings if it contains
// patterns that suggest business logic rather than config/flags.
// These rules should typically stay server-side.
func LintForEdge(rs *compiler.CompiledRuleset) []Warning {
	if rs == nil {
		return nil
	}
	var warnings []Warning

	strs := rs.Constants.Strings()
	resolve := func(idx uint16) string {
		if int(idx) < len(strs) {
			return strs[idx]
		}
		return ""
	}

	// Check for numeric thresholds in the constant pool.
	nums := rs.Constants.Numbers()
	sensitiveNumbers := 0
	for _, n := range nums {
		// Skip trivial values (0, 1, true/false equivalents).
		if n == 0 || n == 1 || n == -1 {
			continue
		}
		sensitiveNumbers++
	}
	if sensitiveNumbers > 3 {
		warnings = append(warnings, Warning{
			Severity: "warn",
			Message:  fmt.Sprintf("bundle contains %d non-trivial numeric constants — these may expose thresholds (pricing, risk scores, limits)", sensitiveNumbers),
		})
	}

	// Check for decimal/currency values.
	decs := rs.Constants.Decimals()
	if len(decs) > 0 {
		warnings = append(warnings, Warning{
			Severity: "warn",
			Message:  fmt.Sprintf("bundle contains %d decimal/currency values — monetary thresholds will be visible in the constant pool", len(decs)),
		})
	}

	// Check for sensitive-looking variable paths.
	sensitivePatterns := []string{
		"risk", "score", "fraud", "price", "amount", "threshold",
		"salary", "income", "credit", "balance", "limit", "budget",
	}
	for _, s := range strs {
		lower := strings.ToLower(s)
		for _, pattern := range sensitivePatterns {
			if strings.Contains(lower, pattern) {
				warnings = append(warnings, Warning{
					Severity: "warn",
					Message:  fmt.Sprintf("variable path %q looks like business logic — consider keeping this rule server-side", s),
				})
				break
			}
		}
	}

	// Check for prerequisite chains (suggest complex business process).
	if len(rs.Prereqs) > 2 {
		warnings = append(warnings, Warning{
			Severity: "info",
			Message:  fmt.Sprintf("bundle has %d prerequisite references — rule dependency chains suggest business process logic", len(rs.Prereqs)),
		})
	}

	// Check for rollout details (should be stripped for edge, warn if not).
	for _, r := range rs.Rules {
		if r.HasRollout && r.RolloutBps > 0 {
			warnings = append(warnings, Warning{
				Severity: "warn",
				Message:  fmt.Sprintf("rule %q has rollout at %d bps — use --obfuscate to strip rollout details", resolve(r.NameIdx), r.RolloutBps),
			})
		}
	}

	return warnings
}
