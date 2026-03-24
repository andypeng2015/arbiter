// Package main implements the arbiter CLI tool.
//
// Commands:
//
//	arbiter check <file.arb>                  — validate without emitting
//	arbiter compile <file.arb>                — compile to bytecode, print stats
//	arbiter eval <file.arb> --data '{...}'    — compile and eval against JSON
//	arbiter strategy <file.arb> [--name Name] --data '{...}' — evaluate one strategy
//	arbiter diff <base.arb> <candidate.arb> [--data '{...}' | --data-file contexts.json] [--key path] [--json] — compare governed outcomes
//	arbiter replay <rules.arb> --audit decisions.jsonl [--request-id id] [--limit N] [--json] — replay audited rule decisions
//	arbiter expert <file.arb> --envelope '{...}' [--facts '[...]'] — run one expert session
//	arbiter test [file.test.arb] [--verbose] — test rules, flags, and scenarios against expected outcomes
//	arbiter fmt <file.arb> [--check]           — format .arb files (--check for CI, exits 1 if unformatted)
//	arbiter bundle <file.arb> [-o output.arbb] [--risk-policy policy.arb] [--force] [--sign key.pem] — export obfuscated binary bundle for edge/browser (policy-gated)
//	arbiter bundle --verify <file.arbb> --pub <public-key.pem> — verify a signed bundle
//	arbiter import <file.json> [-o output.arb] — decompile Arishem JSON to .arb
//	arbiter serve [--grpc :8081] [--audit-file decisions.jsonl] [--bundle-file bundles.json] [--overrides-file overrides.json] — start gRPC API
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/odvcencio/arbiter"
	arbiterv1 "github.com/odvcencio/arbiter/api/arbiter/v1"
	"github.com/odvcencio/arbiter/arbtest"
	"github.com/odvcencio/arbiter/bundle"
	"github.com/odvcencio/arbiter/format"
	"github.com/odvcencio/arbiter/audit"
	"github.com/odvcencio/arbiter/decompile"
	"github.com/odvcencio/arbiter/expert"
	explorepkg "github.com/odvcencio/arbiter/explore"
	"github.com/odvcencio/arbiter/flags"
	"github.com/odvcencio/arbiter/grpcserver"
	"github.com/odvcencio/arbiter/overrides"
	"google.golang.org/grpc"
)

const (
	commandList = "check, compile, eval, fmt, strategy, diff, replay, expert, test, explore, import, serve"
	rootUsage   = "Usage: arbiter <command> <file>\nCommands: " + commandList
)

type usageError string

func (e usageError) Error() string { return string(e) }

var commandHandlers = map[string]func([]string) error{
	"check":    runCheck,
	"compile":  runCompile,
	"fmt":      runFmt,
	"bundle":   runBundle,
	"eval":     runEval,
	"strategy": runStrategy,
	"diff":     runDiff,
	"replay":   runReplay,
	"expert":   runExpert,
	"test":     runTest,
	"explore":  runExplore,
	"import":   runImport,
	"serve":    runServe,
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		var usage usageError
		if errors.As(err, &usage) {
			fmt.Fprintln(os.Stderr, usage.Error())
		} else {
			fmt.Fprintln(os.Stderr, formatCLIError(err))
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError(rootUsage)
	}
	handler, ok := commandHandlers[args[0]]
	if !ok {
		return usageError(fmt.Sprintf("Unknown command: %s\nCommands: %s", args[0], commandList))
	}
	return handler(args[1:])
}

func runCheck(args []string) error {
	if len(args) < 1 {
		return usageError("Usage: arbiter check <file.arb>")
	}
	return check(args[0])
}

func runCompile(args []string) error {
	if len(args) < 1 {
		return usageError("Usage: arbiter compile <file.arb>")
	}
	return compileCmd(args[0])
}

func runEval(args []string) error {
	if len(args) < 1 {
		return usageError("Usage: arbiter eval <file.arb> --data '{...}'")
	}
	dataJSON := ""
	for i := 1; i < len(args); i++ {
		if args[i] == "--data" && i+1 < len(args) {
			dataJSON = args[i+1]
			i++
		}
	}
	if dataJSON == "" {
		return fmt.Errorf("--data flag is required")
	}
	return evalCmd(args[0], dataJSON)
}

func runStrategy(args []string) error {
	if len(args) < 1 {
		return usageError("Usage: arbiter strategy <file.arb> [--name Name] --data '{...}'")
	}
	name := ""
	dataJSON := ""
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--name":
			if i+1 < len(args) {
				name = args[i+1]
				i++
			}
		case "--data":
			if i+1 < len(args) {
				dataJSON = args[i+1]
				i++
			}
		}
	}
	if dataJSON == "" {
		return fmt.Errorf("--data flag is required")
	}
	return strategyCmd(args[0], name, dataJSON)
}

func runDiff(args []string) error {
	if len(args) < 2 {
		return usageError("Usage: arbiter diff <base.arb> <candidate.arb> [--data '{...}' | --data-file contexts.json] [--key path] [--json]")
	}
	dataJSON := ""
	dataFile := ""
	keyPath := ""
	jsonOut := false
	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "--data":
			if i+1 < len(args) {
				dataJSON = args[i+1]
				i++
			}
		case "--data-file":
			if i+1 < len(args) {
				dataFile = args[i+1]
				i++
			}
		case "--key":
			if i+1 < len(args) {
				keyPath = args[i+1]
				i++
			}
		case "--json":
			jsonOut = true
		}
	}
	return diffCmd(args[0], args[1], dataJSON, dataFile, keyPath, jsonOut)
}

func runReplay(args []string) error {
	if len(args) < 1 {
		return usageError("Usage: arbiter replay <rules.arb> --audit decisions.jsonl [--request-id id] [--limit N] [--json]")
	}
	auditPath := ""
	requestID := ""
	limit := 0
	jsonOut := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--audit":
			if i+1 < len(args) {
				auditPath = args[i+1]
				i++
			}
		case "--request-id":
			if i+1 < len(args) {
				requestID = args[i+1]
				i++
			}
		case "--limit":
			if i+1 < len(args) {
				value, err := strconv.Atoi(args[i+1])
				if err != nil {
					return fmt.Errorf("invalid --limit %q: %w", args[i+1], err)
				}
				limit = value
				i++
			}
		case "--json":
			jsonOut = true
		}
	}
	return replayCmd(args[0], auditPath, requestID, limit, jsonOut)
}

func runExpert(args []string) error {
	if len(args) < 1 {
		return usageError("Usage: arbiter expert <file.arb> --envelope '{...}' [--facts '[...]']")
	}
	envelopeJSON := ""
	factsJSON := ""
	for i := 1; i < len(args); i++ {
		if args[i] == "--envelope" && i+1 < len(args) {
			envelopeJSON = args[i+1]
			i++
		}
		if args[i] == "--facts" && i+1 < len(args) {
			factsJSON = args[i+1]
			i++
		}
	}
	if envelopeJSON == "" {
		return fmt.Errorf("--envelope flag is required")
	}
	return expertCmd(args[0], envelopeJSON, factsJSON)
}

func runTest(args []string) error {
	path := ""
	verbose := false
	for _, arg := range args {
		switch arg {
		case "--verbose":
			verbose = true
		default:
			if path == "" {
				path = arg
				continue
			}
			return usageError("Usage: arbiter test [file.test.arb] [--verbose]\n\nWrite a .test.arb file next to your .arb bundle to test rules, flags, and scenarios against expected outcomes.")
		}
	}
	return testCmd(path, verbose)
}

func runImport(args []string) error {
	if len(args) < 1 {
		return usageError("Usage: arbiter import <file.json> [-o output.arb]")
	}
	outPath := ""
	for i := 1; i < len(args); i++ {
		if args[i] == "-o" && i+1 < len(args) {
			outPath = args[i+1]
			i++
		}
	}
	return importCmd(args[0], outPath)
}

func runExplore(args []string) error {
	path := ""
	if len(args) > 0 {
		path = args[0]
	}
	return exploreCmd(path)
}

func runServe(args []string) error {
	grpcAddr := ":8081"
	auditFile := ""
	bundleFile := ""
	overridesFile := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--grpc" && i+1 < len(args) {
			grpcAddr = args[i+1]
			i++
		}
		if args[i] == "--audit-file" && i+1 < len(args) {
			auditFile = args[i+1]
			i++
		}
		if args[i] == "--bundle-file" && i+1 < len(args) {
			bundleFile = args[i+1]
			i++
		}
		if args[i] == "--overrides-file" && i+1 < len(args) {
			overridesFile = args[i+1]
			i++
		}
	}
	return serveCmd(grpcAddr, auditFile, bundleFile, overridesFile)
}

func formatCLIError(err error) string {
	if err == nil {
		return ""
	}
	// Check for multi-error (joined errors). Collect all diagnostic lines.
	if multi := collectDiagnostics(err); len(multi) > 0 {
		return strings.Join(multi, "\n")
	}
	if msg, ok := diagnosticString(err); ok {
		return msg
	}
	return fmt.Sprintf("error: %v", err)
}

func collectDiagnostics(err error) []string {
	// Walk the full error chain looking for joined errors.
	type unwrapMulti interface{ Unwrap() []error }
	if joined, ok := findJoined(err); ok {
		subs := joined.Unwrap()
		var lines []string
		for _, sub := range subs {
			if msg, ok := diagnosticString(sub); ok {
				lines = append(lines, msg)
			}
		}
		return lines
	}
	return nil
}

func findJoined(err error) (interface{ Unwrap() []error }, bool) {
	for cur := err; cur != nil; {
		if j, ok := cur.(interface{ Unwrap() []error }); ok {
			return j, true
		}
		cur = errors.Unwrap(cur)
	}
	return nil, false
}

func diagnosticString(err error) (string, bool) {
	var diag *arbiter.DiagnosticError
	if errors.As(err, &diag) {
		return diag.Error(), true
	}
	for cur := err; cur != nil; cur = errors.Unwrap(cur) {
		if looksLikeDiagnostic(cur.Error()) {
			return cur.Error(), true
		}
	}
	return "", false
}

func looksLikeDiagnostic(message string) bool {
	_, rest, ok := strings.Cut(message, ":")
	if !ok {
		return false
	}
	first, rest, ok := strings.Cut(rest, ":")
	if !ok {
		return false
	}
	if _, err := strconv.Atoi(strings.TrimSpace(first)); err != nil {
		return false
	}
	if second, _, ok := strings.Cut(rest, ":"); ok {
		if _, err := strconv.Atoi(strings.TrimSpace(second)); err == nil {
			return true
		}
	}
	return true
}

func check(path string) error {
	unit, parsed, err := arbiter.LoadFileParsed(path)
	if err != nil {
		return fmt.Errorf("check %s: %w", path, err)
	}
	full, err := arbiter.CompileFullParsed(parsed)
	if err != nil {
		return fmt.Errorf("check %s: %w", path, arbiter.WrapFileError(unit, err))
	}
	if _, err := flags.LoadParsed(parsed, full); err != nil {
		return fmt.Errorf("check %s: %w", path, arbiter.WrapFileError(unit, err))
	}
	if _, err := expert.CompileParsed(parsed, full); err != nil {
		return fmt.Errorf("check %s: %w", path, arbiter.WrapFileError(unit, err))
	}
	if full.Strategies.Count() == 0 && len(full.Workers) == 0 && len(full.Arbiters) == 0 {
		if _, err := arbiter.TranspileParsed(parsed); err != nil {
			return fmt.Errorf("check %s: %w", path, arbiter.WrapFileError(unit, err))
		}
	}

	fmt.Fprintf(os.Stderr, "%s: ok\n", path)
	return nil
}

func compileCmd(path string) error {
	full, err := arbiter.CompileFullFile(path)
	if err != nil {
		return fmt.Errorf("compile %s: %w", path, err)
	}
	rs := full.Ruleset

	fmt.Printf("compiled %s\n", path)
	fmt.Printf("  rules:        %d\n", len(rs.Rules))
	fmt.Printf("  strategies:   %d\n", full.Strategies.Count())
	fmt.Printf("  workers:      %d\n", len(full.Workers))
	fmt.Printf("  arbiters:     %d\n", len(full.Arbiters))
	fmt.Printf("  actions:      %d\n", len(rs.Actions))
	fmt.Printf("  instructions: %d bytes\n", len(rs.Instructions))
	fmt.Printf("  strings:      %d\n", rs.Constants.StringCount())
	fmt.Printf("  numbers:      %d\n", rs.Constants.NumberCount())
	return nil
}

func evalCmd(path, dataJSON string) error {
	prog, err := arbiter.CompileFile(path)
	if err != nil {
		return fmt.Errorf("compile %s: %w", path, err)
	}

	dc, err := arbiter.DataFromJSON(dataJSON, prog)
	if err != nil {
		return fmt.Errorf("parse data: %w", err)
	}

	matched, err := arbiter.Eval(prog, dc)
	if err != nil {
		return fmt.Errorf("eval: %w", err)
	}

	if len(matched) == 0 {
		fmt.Println("no rules matched")
		return nil
	}

	for _, m := range matched {
		tag := "matched"
		if m.Fallback {
			tag = "fallback"
		}
		fmt.Printf("[%s] %s -> %s", tag, m.Name, m.Action)
		if len(m.Params) > 0 {
			out, _ := json.Marshal(m.Params)
			fmt.Printf(" %s", out)
		}
		fmt.Println()
	}

	return nil
}

func strategyCmd(path, name, dataJSON string) error {
	full, err := arbiter.CompileFullFile(path)
	if err != nil {
		return fmt.Errorf("load strategies: %w", err)
	}
	strategies := full.Strategies
	if name == "" {
		names := strategies.Names()
		switch len(names) {
		case 0:
			return fmt.Errorf("bundle contains no strategies")
		case 1:
			name = names[0]
		default:
			return fmt.Errorf("bundle contains multiple strategies; use --name (%s)", strings.Join(names, ", "))
		}
	}

	var ctx map[string]any
	if err := json.Unmarshal([]byte(dataJSON), &ctx); err != nil {
		return fmt.Errorf("parse data: %w", err)
	}

	result, err := arbiter.EvalStrategy(full, name, ctx)
	if err != nil {
		return fmt.Errorf("strategy %s: %w", name, err)
	}
	blob, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal strategy result: %w", err)
	}
	fmt.Println(string(blob))
	return nil
}

func expertCmd(path, envelopeJSON, factsJSON string) error {
	program, err := expert.CompileFile(path)
	if err != nil {
		return fmt.Errorf("compile expert rules: %w", err)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(envelopeJSON), &envelope); err != nil {
		return fmt.Errorf("parse envelope: %w", err)
	}

	facts, err := parseFactsJSON(factsJSON)
	if err != nil {
		return err
	}

	result, err := expert.NewSession(program, envelope, facts, expert.Options{}).Run(context.Background())
	if err != nil {
		return fmt.Errorf("run expert session: %w", err)
	}

	out := map[string]any{
		"outcomes":    result.Outcomes,
		"facts":       result.Facts,
		"activations": result.Activations,
		"rounds":      result.Rounds,
		"mutations":   result.Mutations,
		"stop_reason": result.StopReason,
	}
	blob, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session result: %w", err)
	}
	fmt.Println(string(blob))
	return nil
}

func testCmd(path string, verbose bool) error {
	paths := []string(nil)
	if path == "" {
		files, err := filepath.Glob("*.test.arb")
		if err != nil {
			return fmt.Errorf("resolve tests: %w", err)
		}
		if len(files) == 0 {
			return fmt.Errorf("no .test.arb files found; create one next to your .arb bundle (e.g. pricing.test.arb for pricing.arb)")
		}
		paths = files
	} else {
		paths = append(paths, path)
	}

	totalPassed := 0
	totalFailed := 0
	for _, item := range paths {
		result, err := arbtest.RunFile(item, arbtest.Options{Verbose: verbose})
		if err != nil {
			return fmt.Errorf("test %s: %w", item, err)
		}
		printTestResult(result)
		totalPassed += result.Passed
		totalFailed += result.Failed
	}

	fmt.Printf("test summary: %d passed, %d failed\n", totalPassed, totalFailed)
	if totalFailed > 0 {
		return fmt.Errorf("%d test cases failed", totalFailed)
	}
	return nil
}

func printTestResult(result *arbtest.FileResult) {
	if result == nil {
		return
	}
	fmt.Printf("%s: %d passed, %d failed\n", result.File, result.Passed, result.Failed)
	for _, item := range result.Cases {
		if !result.Verbose && item.Passed {
			continue
		}
		status := "PASS"
		if !item.Passed {
			status = "FAIL"
		}
		fmt.Printf("  [%s] %s %q\n", status, item.Kind, item.Name)
		for _, detail := range item.Details {
			fmt.Printf("    %s\n", detail)
		}
		if item.Error != "" {
			fmt.Printf("    error: %s\n", item.Error)
		}
	}
}

func exploreCmd(path string) error {
	if path == "" {
		files, err := filepath.Glob("*.arb")
		if err != nil {
			return fmt.Errorf("resolve bundle: %w", err)
		}
		switch len(files) {
		case 0:
			return fmt.Errorf("explore requires a bundle path when the current directory has no .arb files")
		case 1:
			path = files[0]
		default:
			return fmt.Errorf("explore requires a bundle path when the current directory has multiple .arb files")
		}
	}

	summary, err := explorepkg.BuildSummaryFile(path)
	if err != nil {
		return fmt.Errorf("explore %s: %w", path, err)
	}
	blob, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal explore summary: %w", err)
	}
	fmt.Println(string(blob))
	return nil
}

func parseFactsJSON(factsJSON string) ([]expert.Fact, error) {
	if factsJSON == "" {
		return nil, nil
	}
	var facts []expert.Fact
	if err := json.Unmarshal([]byte(factsJSON), &facts); err != nil {
		return nil, fmt.Errorf("parse facts: %w", err)
	}
	return facts, nil
}

// importRuleJSON is the expected JSON structure for each rule in the input file.
type importRuleJSON struct {
	Name      string `json:"name"`
	Priority  int    `json:"priority"`
	Condition any    `json:"condition"`
	Action    any    `json:"action,omitempty"`
	Fallback  any    `json:"fallback,omitempty"`
}

func importCmd(path, outPath string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	// Try parsing as the TranspileResult format (with "rules" key)
	var wrapper struct {
		Rules []importRuleJSON `json:"rules"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil && len(wrapper.Rules) > 0 {
		return importRules(wrapper.Rules, outPath)
	}

	// Try parsing as a bare array of rules
	var ruleArr []importRuleJSON
	if err := json.Unmarshal(data, &ruleArr); err == nil && len(ruleArr) > 0 {
		return importRules(ruleArr, outPath)
	}

	// Try parsing as a single rule
	var single importRuleJSON
	if err := json.Unmarshal(data, &single); err == nil && single.Name != "" {
		return importRules([]importRuleJSON{single}, outPath)
	}

	return fmt.Errorf("cannot parse %s: expected Arishem JSON with rules array, rule array, or single rule", path)
}

func serveCmd(grpcAddr, auditFile, bundleFile, overridesFile string) error {
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", grpcAddr, err)
	}

	var sink audit.Sink = audit.NopSink{}
	var closer interface{ Close() error }
	if auditFile != "" {
		fileSink, err := audit.NewJSONLSink(auditFile)
		if err != nil {
			return fmt.Errorf("open audit sink: %w", err)
		}
		sink = fileSink
		closer = fileSink
	}
	if closer != nil {
		defer closer.Close()
	}

	registry := grpcserver.NewRegistry()
	if bundleFile != "" {
		registry, err = grpcserver.NewFileRegistry(bundleFile)
		if err != nil {
			return fmt.Errorf("open bundle registry: %w", err)
		}
	}
	store := overrides.NewStore()
	if overridesFile != "" {
		store, err = overrides.NewFileStore(overridesFile)
		if err != nil {
			return fmt.Errorf("open override store: %w", err)
		}
	}

	grpcSrv := grpc.NewServer()
	arbiterv1.RegisterArbiterServiceServer(grpcSrv, grpcserver.NewServer(registry, store, sink))

	fmt.Fprintf(os.Stderr, "arbiter gRPC listening on %s\n", grpcAddr)
	return grpcSrv.Serve(lis)
}

func importRules(rules []importRuleJSON, outPath string) error {
	var arishemRules []decompile.ArishemRule

	for _, r := range rules {
		ar := decompile.ArishemRule{
			Name:     r.Name,
			Priority: r.Priority,
		}

		if r.Condition != nil {
			b, err := json.Marshal(r.Condition)
			if err != nil {
				return fmt.Errorf("rule %s: marshal condition: %w", r.Name, err)
			}
			ar.Condition = string(b)
		}
		if r.Action != nil {
			b, err := json.Marshal(r.Action)
			if err != nil {
				return fmt.Errorf("rule %s: marshal action: %w", r.Name, err)
			}
			ar.Action = string(b)
		}
		if r.Fallback != nil {
			b, err := json.Marshal(r.Fallback)
			if err != nil {
				return fmt.Errorf("rule %s: marshal fallback: %w", r.Name, err)
			}
			ar.Fallback = string(b)
		}

		arishemRules = append(arishemRules, ar)
	}

	arb, err := decompile.ArishemToArb(arishemRules)
	if err != nil {
		return fmt.Errorf("decompile: %w", err)
	}

	if outPath != "" {
		if err := os.WriteFile(outPath, []byte(arb), 0644); err != nil {
			return fmt.Errorf("write %s: %w", outPath, err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s (%d rules)\n", outPath, len(rules))
		return nil
	}

	fmt.Print(arb)
	return nil
}

func runFmt(args []string) error {
	check := false
	var paths []string
	for _, arg := range args {
		switch arg {
		case "--check":
			check = true
		case "--write":
			// Default behavior — explicit flag accepted for clarity.
		default:
			paths = append(paths, arg)
		}
	}
	if len(paths) == 0 {
		return usageError("Usage: arbiter fmt [--check] [--write] <file.arb> [file2.arb ...]")
	}
	changed := false
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		formatted := format.Format(source)
		if string(formatted) == string(source) {
			continue
		}
		changed = true
		if check {
			fmt.Fprintf(os.Stderr, "%s: needs formatting\n", path)
			continue
		}
		if err := os.WriteFile(path, formatted, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Fprintf(os.Stderr, "formatted %s\n", path)
	}
	if check && changed {
		return fmt.Errorf("files need formatting")
	}
	return nil
}

func runBundle(args []string) error {
	if len(args) < 1 {
		return usageError("Usage: arbiter bundle <file.arb> [-o output.arbb] [--risk-policy policy.arb] [--force] [--sign key.pem]\n       arbiter bundle --verify <file.arbb> --pub <public-key.pem>")
	}

	// Check for --verify mode first.
	verifyPath := ""
	pubKeyPath := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--verify":
			if i+1 < len(args) {
				verifyPath = args[i+1]
				i++
			}
		case "--pub":
			if i+1 < len(args) {
				pubKeyPath = args[i+1]
				i++
			}
		}
	}
	if verifyPath != "" {
		return verifyBundle(verifyPath, pubKeyPath)
	}

	path := args[0]
	outPath := ""
	riskPolicyPath := ""
	signKeyPath := ""
	force := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 < len(args) {
				outPath = args[i+1]
				i++
			}
		case "--risk-policy":
			if i+1 < len(args) {
				riskPolicyPath = args[i+1]
				i++
			}
		case "--sign":
			if i+1 < len(args) {
				signKeyPath = args[i+1]
				i++
			}
		case "--force":
			force = true
		}
	}
	prog, err := arbiter.CompileFile(path)
	if err != nil {
		return fmt.Errorf("bundle %s: %w", path, err)
	}

	// Analyze bundle for edge export safety using Arbiter policy.
	var policySource []byte
	if riskPolicyPath != "" {
		policySource, err = os.ReadFile(riskPolicyPath)
		if err != nil {
			return fmt.Errorf("read risk policy %s: %w", riskPolicyPath, err)
		}
	}
	result, err := bundle.Analyze(prog.Ruleset, policySource)
	if err != nil {
		return fmt.Errorf("analyze %s: %w", path, err)
	}

	// Print signals.
	for _, sig := range result.Signals {
		fmt.Fprintf(os.Stderr, "[signal] %s: count=%d\n", sig.Kind, sig.Count)
	}
	// Print decision.
	for _, reason := range result.Reasons {
		fmt.Fprintf(os.Stderr, "[%s] %s\n", result.Decision, reason)
	}
	if result.Decision == "block" && !force {
		return fmt.Errorf("edge export blocked by policy — use --force to override")
	}
	if result.Decision == "warn" {
		fmt.Fprintln(os.Stderr, "[warn] proceeding with warnings")
	}

	opts := bundle.ObfuscateOptions{
		HashRuleNames:       true,
		HashSegmentNames:    true,
		StripRolloutDetails: true,
		StripPrereqs:        true,
	}
	blob, err := bundle.MarshalObfuscated(prog.Ruleset, opts)
	if err != nil {
		return fmt.Errorf("bundle %s: %w", path, err)
	}

	// Sign the bundle if --sign was provided.
	if signKeyPath != "" {
		privKey, err := loadPrivateKey(signKeyPath)
		if err != nil {
			return fmt.Errorf("load signing key: %w", err)
		}
		blob, err = bundle.Sign(blob, privKey)
		if err != nil {
			return fmt.Errorf("sign bundle: %w", err)
		}
		fmt.Fprintln(os.Stderr, "[sign] bundle signed with Ed25519")
	}

	if outPath == "" {
		outPath = strings.TrimSuffix(path, ".arb") + ".arbb"
	}
	if err := os.WriteFile(outPath, blob, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d rules, %d bytes, obfuscated)\n", outPath, len(prog.Ruleset.Rules), len(blob))
	return nil
}

// verifyBundle reads a signed .arbb file and verifies it against the given
// public key. Prints the verification result to stderr.
func verifyBundle(bundlePath, pubKeyPath string) error {
	if pubKeyPath == "" {
		return fmt.Errorf("--pub flag is required for --verify")
	}
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		return fmt.Errorf("read bundle %s: %w", bundlePath, err)
	}
	pubKey, err := loadPublicKey(pubKeyPath)
	if err != nil {
		return fmt.Errorf("load public key: %w", err)
	}
	payload, err := bundle.Verify(data, pubKey)
	if err != nil {
		return fmt.Errorf("verify %s: %w", bundlePath, err)
	}
	fmt.Fprintf(os.Stderr, "verified %s: signature valid (%d byte payload)\n", bundlePath, len(payload))
	return nil
}

// loadPrivateKey reads a PEM-encoded Ed25519 private key from disk.
// Accepts PEM files with type "PRIVATE KEY" or "ED25519 PRIVATE KEY"
// containing the raw 64-byte seed+key, or a raw 64-byte file.
func loadPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block != nil {
		if len(block.Bytes) == ed25519.PrivateKeySize {
			return ed25519.PrivateKey(block.Bytes), nil
		}
		// Some PEM encodings wrap just the 32-byte seed.
		if len(block.Bytes) == ed25519.SeedSize {
			return ed25519.NewKeyFromSeed(block.Bytes), nil
		}
		return nil, fmt.Errorf("PEM block has unexpected size %d (want %d or %d)", len(block.Bytes), ed25519.PrivateKeySize, ed25519.SeedSize)
	}
	// Fall back to raw bytes.
	if len(data) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(data), nil
	}
	if len(data) == ed25519.SeedSize {
		return ed25519.NewKeyFromSeed(data), nil
	}
	return nil, fmt.Errorf("file %s is not a valid Ed25519 private key (size %d)", path, len(data))
}

// loadPublicKey reads a PEM-encoded Ed25519 public key from disk.
// Accepts PEM files with type "PUBLIC KEY" or "ED25519 PUBLIC KEY"
// containing the raw 32-byte key, or a raw 32-byte file.
func loadPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block != nil {
		if len(block.Bytes) == ed25519.PublicKeySize {
			return ed25519.PublicKey(block.Bytes), nil
		}
		return nil, fmt.Errorf("PEM block has unexpected size %d (want %d)", len(block.Bytes), ed25519.PublicKeySize)
	}
	if len(data) == ed25519.PublicKeySize {
		return ed25519.PublicKey(data), nil
	}
	return nil, fmt.Errorf("file %s is not a valid Ed25519 public key (size %d)", path, len(data))
}
