package arbiter

import (
	"encoding/json"
	"fmt"

	"m31labs.dev/arbiter/ir"
)

// TranspileResult holds the parsed output of an .arb file.
type TranspileResult struct {
	Features map[string]Feature `json:"features,omitempty"`
	Consts   map[string]any     `json:"consts,omitempty"`
	Rules    []RuleOutput       `json:"rules"`
}

type Feature struct {
	Name   string            `json:"name"`
	Source string            `json:"source"`
	Fields map[string]string `json:"fields"`
}

type RuleOutput struct {
	Name      string `json:"name"`
	Priority  int    `json:"priority,omitempty"`
	Condition any    `json:"condition"`
	Action    any    `json:"action"`
	Fallback  any    `json:"fallback,omitempty"`
}

// Transpile converts .arb source to Arishem-compatible JSON.
//
// Deprecated: the Arishem JSON emit direction is deprecated and will be removed
// in v2.0.0 — Arbiter is the engine, not a transpiler to other engines. Use
// `arbiter import` (decompile) as the migration on-ramp.
func Transpile(source []byte) (string, error) {
	parsed, err := ParseSource(source)
	if err != nil {
		return "", err
	}
	return TranspileParsed(parsed)
}

// TranspileParsed converts a previously parsed .arb program to Arishem-compatible JSON.
//
// Deprecated: the Arishem JSON emit direction is deprecated and will be removed
// in v2.0.0. Use `arbiter import` (decompile) as the migration on-ramp.
func TranspileParsed(parsed *ParsedSource) (string, error) {
	if parsed == nil {
		return "", fmt.Errorf("nil parsed source")
	}
	program, err := ir.Lower(parsed.Root, parsed.Source, parsed.Lang)
	if err != nil {
		return "", err
	}
	if len(program.Strategies) > 0 {
		return "", fmt.Errorf("bundle contains strategy declarations; Arishem JSON emit only supports rules")
	}
	if len(program.Workers) > 0 {
		return "", fmt.Errorf("bundle contains worker declarations; Arishem JSON emit only supports rules")
	}
	if len(program.Arbiters) > 0 {
		return "", fmt.Errorf("bundle contains arbiter declarations; Arishem JSON emit only supports rules")
	}
	result := emitIRProgram(program)
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal JSON: %w", err)
	}
	return string(out), nil
}

// TranspileFile resolves includes and transpiles a file-backed .arb program.
//
// Deprecated: Arishem JSON emit; will be removed in v2.0.0.
func TranspileFile(path string) (string, error) {
	unit, parsed, err := LoadFileParsed(path)
	if err != nil {
		return "", err
	}
	out, err := TranspileParsed(parsed)
	if err != nil {
		return "", WrapFileError(unit, err)
	}
	return out, nil
}

// TranspileRule converts a single rule's condition to Arishem JSON (no wrapper).
//
// Deprecated: Arishem JSON emit; will be removed in v2.0.0.
func TranspileRule(source []byte, ruleName string) (string, error) {
	parsed, err := ParseSource(source)
	if err != nil {
		return "", err
	}
	return TranspileRuleParsed(parsed, ruleName)
}

// TranspileRuleParsed converts one rule condition from a parsed .arb program.
//
// Deprecated: Arishem JSON emit; will be removed in v2.0.0.
func TranspileRuleParsed(parsed *ParsedSource, ruleName string) (string, error) {
	if parsed == nil {
		return "", fmt.Errorf("nil parsed source")
	}
	program, err := ir.Lower(parsed.Root, parsed.Source, parsed.Lang)
	if err != nil {
		return "", err
	}
	rule, ok := program.RuleByName(ruleName)
	if !ok {
		return "", fmt.Errorf("rule %q not found", ruleName)
	}
	cond := emitIRRuleCondition(program, rule)
	out, err := json.MarshalIndent(cond, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// TranspileRuleFile resolves includes and transpiles one rule from a file-backed
// .arb program.
//
// Deprecated: Arishem JSON emit; will be removed in v2.0.0.
func TranspileRuleFile(path, ruleName string) (string, error) {
	unit, parsed, err := LoadFileParsed(path)
	if err != nil {
		return "", err
	}
	out, err := TranspileRuleParsed(parsed, ruleName)
	if err != nil {
		return "", WrapFileError(unit, err)
	}
	return out, nil
}
