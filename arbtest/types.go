package arbtest

import "time"

// Suite is one parsed .test.arb file.
type Suite struct {
	File      string
	Tests     []TestCase
	Scenarios []Scenario
}

// TestCase is one stateless assertion test.
type TestCase struct {
	Name         string
	Given        map[string]any
	Expectations []Expectation
}

// Scenario is one expert-session scenario.
type Scenario struct {
	Name       string
	Given      map[string]any
	Assertions []FactSpec
	Steps      []ScenarioStep
}

// StreamEvent is one simulated continuous-arbiter input event.
type StreamEvent struct {
	Name   string
	Fields map[string]any
}

// ScenarioStep is one scenario phase.
type ScenarioStep struct {
	AtOffset     time.Duration
	HasAtOffset  bool
	Stabilize    bool
	WithinWindow time.Duration
	HasWithin    bool
	Given        map[string]any
	Assertions   []FactSpec
	StreamEvents []StreamEvent
	Expectations []Expectation
}

// FactSpec is one asserted fact in a scenario.
type FactSpec struct {
	Type   string
	Key    string
	Fields map[string]any
}

// ExpectationKind identifies the thing being asserted.
type ExpectationKind string

const (
	ExpectRule     ExpectationKind = "rule"
	ExpectAction   ExpectationKind = "action"
	ExpectFlag     ExpectationKind = "flag"
	ExpectFact     ExpectationKind = "fact"
	ExpectOutcome  ExpectationKind = "outcome"
	ExpectStrategy ExpectationKind = "strategy"
)

// FieldMatchKind identifies how a field expectation is evaluated.
type FieldMatchKind string

const (
	FieldExact   FieldMatchKind = "exact"
	FieldGt      FieldMatchKind = "gt"
	FieldGte     FieldMatchKind = "gte"
	FieldLt      FieldMatchKind = "lt"
	FieldLte     FieldMatchKind = "lte"
	FieldBetween FieldMatchKind = "between"
)

// FieldExpectation describes one expected field value or comparison.
type FieldExpectation struct {
	Kind  FieldMatchKind
	Value any
	High  any
}

// Expectation is one parsed expectation statement.
type Expectation struct {
	Kind        ExpectationKind
	Target      string
	Selected    string // strategy candidate label
	Negated     bool
	RuleMatched bool
	Value       any
	Fields      map[string]FieldExpectation
}

// Options control execution.
type Options struct {
	Verbose bool
}

// FileResult is the execution result for one .test.arb file.
type FileResult struct {
	File     string
	Bundle   string
	Cases    []CaseResult
	Passed   int
	Failed   int
	Verbose  bool
	Coverage Coverage
}

// CaseResult is the result of one test or scenario.
type CaseResult struct {
	Name    string
	Kind    string
	Passed  bool
	Details []string
	Error   string
	// CoveredRules names the rules this case exercises — those it matched plus
	// those named in a rule expectation (so negative tests count too).
	CoveredRules []string
}

// Coverage reports which of a bundle's rules were exercised by its tests.
type Coverage struct {
	Total     int
	Covered   []string // rule names exercised by at least one test (sorted)
	Uncovered []string // rule names no test exercises (sorted)
}

// Percent is the fraction of rules covered, 0–100. An empty ruleset is 100%.
func (c Coverage) Percent() float64 {
	if c.Total == 0 {
		return 100
	}
	return float64(len(c.Covered)) / float64(c.Total) * 100
}
