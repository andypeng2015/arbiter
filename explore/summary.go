package explore

import (
	"fmt"
	"sort"

	arbiter "m31labs.dev/arbiter"
	"m31labs.dev/arbiter/ir"
	"m31labs.dev/arbiter/units"
)

// Summary is a bundle-level semantic summary for inspection surfaces.
type Summary struct {
	Source           string                   `json:"source,omitempty"`
	DataDeclarations []DataDeclarationSummary `json:"data_declarations,omitempty"`
	FactSchemas      []SchemaSummary          `json:"fact_schemas,omitempty"`
	OutcomeSchemas   []SchemaSummary          `json:"outcome_schemas,omitempty"`
	Strategies       []StrategySummary        `json:"strategies,omitempty"`
	Workers          []WorkerSummary          `json:"workers,omitempty"`
	Arbiters         []ArbiterSummary         `json:"arbiters,omitempty"`
	Constants        []ConstantSummary        `json:"constants,omitempty"`
	Rules            []RuleSummary            `json:"rules,omitempty"`
	ExpertRules      []ExpertRuleSummary      `json:"expert_rules,omitempty"`
	UsedUnits        []DimensionUnits         `json:"used_units,omitempty"`
}

type SchemaSummary struct {
	Name   string         `json:"name"`
	Fields []FieldSummary `json:"fields,omitempty"`
}

type DataDeclarationSummary struct {
	Kind   string             `json:"kind"`
	Name   string             `json:"name,omitempty"`
	Source string             `json:"source,omitempty"`
	Fields []DataFieldSummary `json:"fields,omitempty"`
	Rows   int                `json:"rows,omitempty"`
}

type FieldSummary struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

type DataFieldSummary struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required *bool  `json:"required,omitempty"`
}

type ConstantSummary struct {
	Name  string `json:"name"`
	Value any    `json:"value,omitempty"`
	Raw   string `json:"raw"`
}

type RuleSummary struct {
	Name        string             `json:"name"`
	Tags        []string           `json:"tags,omitempty"`
	Priority    int                `json:"priority"`
	Segment     string             `json:"segment,omitempty"`
	KillSwitch  ir.KillSwitchState `json:"kill_switch,omitempty"`
	ActiveFrom  string             `json:"active_from,omitempty"`
	ActiveUntil string             `json:"active_until,omitempty"`
	Action      string             `json:"action"`
}

type ExpertRuleSummary struct {
	Name            string             `json:"name"`
	Tags            []string           `json:"tags,omitempty"`
	Priority        int                `json:"priority"`
	Kind            string             `json:"kind"`
	Target          string             `json:"target"`
	KillSwitch      ir.KillSwitchState `json:"kill_switch,omitempty"`
	PerFact         bool               `json:"per_fact,omitempty"`
	NoLoop          bool               `json:"no_loop,omitempty"`
	Stable          bool               `json:"stable,omitempty"`
	ActivationGroup string             `json:"activation_group,omitempty"`
	For             string             `json:"for,omitempty"`
	Within          string             `json:"within,omitempty"`
	StableFor       string             `json:"stable_for,omitempty"`
	Cooldown        string             `json:"cooldown,omitempty"`
	Debounce        string             `json:"debounce,omitempty"`
}

type StrategySummary struct {
	Name       string                     `json:"name"`
	Returns    string                     `json:"returns"`
	Candidates []StrategyCandidateSummary `json:"candidates,omitempty"`
}

type StrategyCandidateSummary struct {
	Label       string             `json:"label"`
	Condition   string             `json:"condition,omitempty"`
	Segment     string             `json:"segment,omitempty"`
	KillSwitch  ir.KillSwitchState `json:"kill_switch,omitempty"`
	ActiveFrom  string             `json:"active_from,omitempty"`
	ActiveUntil string             `json:"active_until,omitempty"`
	Rollout     string             `json:"rollout,omitempty"`
	Else        bool               `json:"else,omitempty"`
}

type WorkerSummary struct {
	Name   string `json:"name"`
	Input  string `json:"input"`
	Output string `json:"output"`
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
}

type ArbiterSummary struct {
	Name       string                  `json:"name"`
	Triggers   []ArbiterTriggerSummary `json:"triggers,omitempty"`
	Sources    []string                `json:"sources,omitempty"`
	Checkpoint string                  `json:"checkpoint,omitempty"`
	Handlers   []ArbiterHandlerSummary `json:"handlers,omitempty"`
}

type ArbiterTriggerSummary struct {
	Kind     string `json:"kind"`
	Interval string `json:"interval,omitempty"`
	Schedule string `json:"schedule,omitempty"`
	Target   string `json:"target,omitempty"`
}

type ArbiterHandlerSummary struct {
	Outcome string `json:"outcome"`
	Where   string `json:"where,omitempty"`
	Kind    string `json:"kind"`
	Target  string `json:"target,omitempty"`
}

type DimensionUnits struct {
	Dimension string   `json:"dimension"`
	Symbols   []string `json:"symbols,omitempty"`
}

// BuildSummaryFile compiles one bundle file and returns its semantic summary.
func BuildSummaryFile(path string) (*Summary, error) {
	full, err := arbiter.CompileFullFile(path)
	if err != nil {
		return nil, err
	}
	if full == nil || full.Program == nil {
		return nil, fmt.Errorf("nil compiled program")
	}
	summary := BuildSummary(full.Program)
	summary.Source = path
	return summary, nil
}

// BuildSummary creates an inspection summary from a lowered program.
func BuildSummary(program *ir.Program) *Summary {
	if program == nil {
		return &Summary{}
	}
	summary := &Summary{}
	if program.Input != nil {
		summary.DataDeclarations = append(summary.DataDeclarations, summarizeInputSchema(*program.Input))
	}
	for _, feature := range program.Features {
		summary.DataDeclarations = append(summary.DataDeclarations, summarizeFeature(feature))
	}
	for _, schema := range program.FactSchemas {
		summary.DataDeclarations = append(summary.DataDeclarations, summarizeNamedSchema("fact", schema.Name, schema.Fields))
		summary.FactSchemas = append(summary.FactSchemas, summarizeFactSchema(schema))
	}
	for _, schema := range program.OutcomeSchemas {
		summary.DataDeclarations = append(summary.DataDeclarations, summarizeNamedSchema("outcome", schema.Name, schema.Fields))
		summary.OutcomeSchemas = append(summary.OutcomeSchemas, summarizeOutcomeSchema(schema))
	}
	for _, table := range program.Tables {
		summary.DataDeclarations = append(summary.DataDeclarations, summarizeTable(table))
	}
	for _, strategy := range program.Strategies {
		summary.Strategies = append(summary.Strategies, summarizeStrategy(program, strategy))
	}
	for _, worker := range program.Workers {
		summary.Workers = append(summary.Workers, WorkerSummary{
			Name:   worker.Name,
			Input:  worker.Input,
			Output: worker.Output,
			Kind:   worker.Kind,
			Target: worker.Target,
		})
	}
	for _, arbiterDecl := range program.Arbiters {
		summary.Arbiters = append(summary.Arbiters, summarizeArbiter(program, arbiterDecl))
	}
	for _, decl := range program.Consts {
		value, ok := ir.LiteralValue(program, decl.Value)
		item := ConstantSummary{
			Name: decl.Name,
			Raw:  ir.RenderExpr(program, decl.Value),
		}
		if ok {
			item.Value = value
		}
		summary.Constants = append(summary.Constants, item)
	}
	for _, rule := range program.Rules {
		summary.Rules = append(summary.Rules, RuleSummary{
			Name:        rule.Name,
			Tags:        append([]string(nil), rule.Tags...),
			Priority:    int(rule.Priority),
			Segment:     rule.Segment,
			KillSwitch:  rule.KillSwitch,
			ActiveFrom:  activeFrom(rule.ActiveWindow),
			ActiveUntil: activeUntil(rule.ActiveWindow),
			Action:      rule.Action.Name,
		})
	}
	for _, rule := range program.Expert {
		item := ExpertRuleSummary{
			Name:            rule.Name,
			Tags:            append([]string(nil), rule.Tags...),
			Priority:        int(rule.Priority),
			Kind:            string(rule.ActionKind),
			Target:          rule.Target,
			KillSwitch:      rule.KillSwitch,
			PerFact:         rule.PerFact,
			NoLoop:          rule.NoLoop,
			Stable:          rule.Stable,
			ActivationGroup: rule.ActivationGroup,
		}
		if rule.ForDuration != nil {
			item.For = formatDuration(rule.ForDuration)
		}
		if rule.WithinDuration != nil {
			item.Within = formatDuration(rule.WithinDuration)
		}
		if rule.HasStableCycles {
			item.StableFor = fmt.Sprintf("%d cycles", rule.StableCycles)
		}
		if rule.CooldownDuration != nil {
			item.Cooldown = formatDuration(rule.CooldownDuration)
		}
		if rule.DebounceDuration != nil {
			item.Debounce = formatDuration(rule.DebounceDuration)
		}
		summary.ExpertRules = append(summary.ExpertRules, item)
	}
	summary.UsedUnits = collectUsedUnits(program)
	return summary
}

func summarizeFactSchema(schema ir.FactSchema) SchemaSummary {
	out := SchemaSummary{
		Name: schema.Name,
	}
	for _, field := range schema.Fields {
		out.Fields = append(out.Fields, summarizeSchemaField(field.Name, field))
	}
	return out
}

func summarizeOutcomeSchema(schema ir.OutcomeSchema) SchemaSummary {
	out := SchemaSummary{
		Name: schema.Name,
	}
	for _, field := range schema.Fields {
		out.Fields = append(out.Fields, summarizeSchemaField(field.Name, field))
	}
	return out
}

func summarizeInputSchema(schema ir.InputSchema) DataDeclarationSummary {
	return DataDeclarationSummary{
		Kind:   "input",
		Fields: summarizeDataSchemaFields(schema.Fields),
	}
}

func summarizeFeature(feature ir.Feature) DataDeclarationSummary {
	out := DataDeclarationSummary{
		Kind:   "feature",
		Name:   feature.Name,
		Source: feature.Source,
	}
	for _, field := range feature.Fields {
		out.Fields = append(out.Fields, DataFieldSummary{
			Name: field.Name,
			Type: field.Type,
		})
	}
	return out
}

func summarizeNamedSchema(kind, name string, fields []ir.SchemaField) DataDeclarationSummary {
	return DataDeclarationSummary{
		Kind:   kind,
		Name:   name,
		Fields: summarizeDataSchemaFields(fields),
	}
}

func summarizeTable(table ir.Table) DataDeclarationSummary {
	out := DataDeclarationSummary{
		Kind: "table",
		Name: table.Name,
		Rows: len(table.Rows),
	}
	for _, column := range table.Columns {
		out.Fields = append(out.Fields, DataFieldSummary{
			Name: column.Name,
			Type: fieldTypeString(column.Type, true),
		})
	}
	return out
}

func summarizeSchemaField(name string, field ir.SchemaField) FieldSummary {
	return FieldSummary{
		Name:     name,
		Type:     fieldTypeString(field.Type, field.Required),
		Required: field.Required,
	}
}

func summarizeDataSchemaFields(fields []ir.SchemaField) []DataFieldSummary {
	return appendDataSchemaFields(nil, "", fields)
}

func appendDataSchemaFields(dst []DataFieldSummary, prefix string, fields []ir.SchemaField) []DataFieldSummary {
	for _, field := range fields {
		name := field.Name
		if prefix != "" {
			name = prefix + "." + name
		}
		required := field.Required
		dst = append(dst, DataFieldSummary{
			Name:     name,
			Type:     fieldTypeString(field.Type, field.Required),
			Required: &required,
		})
		if len(field.Children) > 0 {
			dst = appendDataSchemaFields(dst, name, field.Children)
		}
	}
	return dst
}

func summarizeStrategy(program *ir.Program, strategy ir.Strategy) StrategySummary {
	out := StrategySummary{
		Name:    strategy.Name,
		Returns: strategy.Returns,
	}
	for _, candidate := range strategy.Candidates {
		item := StrategyCandidateSummary{
			Label:       candidate.Label,
			Segment:     candidate.Segment,
			KillSwitch:  candidate.KillSwitch,
			ActiveFrom:  activeFrom(candidate.ActiveWindow),
			ActiveUntil: activeUntil(candidate.ActiveWindow),
			Else:        candidate.IsElse,
		}
		if candidate.HasCondition {
			item.Condition = ir.RenderExpr(program, candidate.Condition)
		}
		if candidate.Rollout != nil {
			item.Rollout = formatRollout(candidate.Rollout)
		}
		out.Candidates = append(out.Candidates, item)
	}
	return out
}

func summarizeArbiter(program *ir.Program, arbiterDecl ir.Arbiter) ArbiterSummary {
	out := ArbiterSummary{Name: arbiterDecl.Name}
	for _, clause := range arbiterDecl.Clauses {
		switch clause.Kind {
		case ir.ArbiterPollClause:
			out.Triggers = append(out.Triggers, ArbiterTriggerSummary{
				Kind:     string(clause.Kind),
				Interval: clause.Interval,
			})
		case ir.ArbiterStreamClause:
			out.Triggers = append(out.Triggers, ArbiterTriggerSummary{
				Kind:   string(clause.Kind),
				Target: clause.Target,
			})
		case ir.ArbiterScheduleClause:
			out.Triggers = append(out.Triggers, ArbiterTriggerSummary{
				Kind:     string(clause.Kind),
				Schedule: clause.Expr,
				Target:   clause.Target,
			})
		case ir.ArbiterSourceClause:
			out.Sources = append(out.Sources, clause.Target)
		case ir.ArbiterCheckpointClause:
			out.Checkpoint = clause.Target
		case ir.ArbiterHandlerClause:
			item := ArbiterHandlerSummary{
				Outcome: clause.Outcome,
				Kind:    clause.Handler,
				Target:  clause.Target,
			}
			if clause.HasFilter {
				item.Where = ir.RenderExpr(program, clause.Filter)
			}
			out.Handlers = append(out.Handlers, item)
		}
	}
	return out
}

func fieldTypeString(fieldType ir.FieldType, required bool) string {
	base := renderFieldType(fieldType)
	if !required {
		return base + "?"
	}
	return base
}

func renderFieldType(fieldType ir.FieldType) string {
	base := fieldType.Base
	if fieldType.Base == "list" && fieldType.Element != nil {
		base = "list<" + renderFieldType(*fieldType.Element) + ">"
	}
	if fieldType.Dimension != "" {
		base += "<" + fieldType.Dimension + ">"
	}
	return base
}

func formatDuration(duration *ir.Duration) string {
	if duration == nil {
		return ""
	}
	return fmt.Sprintf("%g%s", duration.Value, duration.Unit)
}

func formatRollout(rollout *ir.Rollout) string {
	if rollout == nil {
		return ""
	}
	out := fmt.Sprintf("%g%%", float64(rollout.Bps)/100)
	if rollout.HasSubject {
		out += " by " + rollout.Subject
	}
	if rollout.HasNamespace {
		out += fmt.Sprintf(" namespace %q", rollout.Namespace)
	}
	return out
}

func activeFrom(window ir.ActiveWindow) string {
	if !window.HasFrom {
		return ""
	}
	return window.From
}

func activeUntil(window ir.ActiveWindow) string {
	if !window.HasUntil {
		return ""
	}
	return window.Until
}

func collectUsedUnits(program *ir.Program) []DimensionUnits {
	dimensions := make(map[string]map[string]struct{})
	add := func(dimension string, symbols []string) {
		if dimension == "" {
			return
		}
		bySymbol := dimensions[dimension]
		if bySymbol == nil {
			bySymbol = make(map[string]struct{})
			dimensions[dimension] = bySymbol
		}
		for _, symbol := range symbols {
			bySymbol[symbol] = struct{}{}
		}
	}

	for _, schema := range program.FactSchemas {
		for _, field := range schema.Fields {
			add(field.Type.Dimension, units.SymbolsForDimension(field.Type.Dimension))
		}
	}
	for _, schema := range program.OutcomeSchemas {
		for _, field := range schema.Fields {
			add(field.Type.Dimension, units.SymbolsForDimension(field.Type.Dimension))
		}
	}
	for _, expr := range program.Exprs {
		switch expr.Kind {
		case ir.ExprQuantityLit, ir.ExprDecimalLit:
			entry, ok := units.Lookup(expr.Unit)
			if !ok {
				continue
			}
			add(entry.Dimension, []string{entry.Symbol})
		}
	}

	if len(dimensions) == 0 {
		return nil
	}
	keys := make([]string, 0, len(dimensions))
	for dimension := range dimensions {
		keys = append(keys, dimension)
	}
	sort.Strings(keys)

	out := make([]DimensionUnits, 0, len(keys))
	for _, dimension := range keys {
		symbols := make([]string, 0, len(dimensions[dimension]))
		for symbol := range dimensions[dimension] {
			symbols = append(symbols, symbol)
		}
		sort.Strings(symbols)
		out = append(out, DimensionUnits{
			Dimension: dimension,
			Symbols:   symbols,
		})
	}
	return out
}
