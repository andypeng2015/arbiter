package main

import (
	"encoding/json"
	"sort"
	"strings"

	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/ir"
)

// semanticTokenTypes is the legend for semantic token types.
// The index of each entry is referenced by encoded tokens.
var semanticTokenTypes = []string{
	"namespace", // 0 - module prefix in qualified names
	"type",      // 1 - fact, outcome names
	"struct",    // 2 - table names
	"property",  // 3 - field access in member chains
	"variable",  // 4 - rule, flag, expert rule names (reference sites)
	"function",  // 5 - segment names (reference sites)
}

var semanticTokenModifiers = []string{
	"declaration", // 0
	"readonly",    // 1
}

// semanticToken represents a single token before delta encoding.
type semanticToken struct {
	line      int
	col       int
	length    int
	tokenType int
	modifiers int
}

// handleSemanticTokens handles the textDocument/semanticTokens/full request.
func (s *server) handleSemanticTokens(msg rpcMessage) rpcMessage {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	json.Unmarshal(msg.Params, &params)

	s.mu.Lock()
	f, ok := s.files[params.TextDocument.URI]
	if !ok {
		s.mu.Unlock()
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: map[string]any{"data": []int{}}}
	}
	content := f.content
	s.mu.Unlock()

	source := []byte(content)
	full, err := arbiter.CompileFull(source)
	if err != nil || full == nil || full.Program == nil {
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: map[string]any{"data": []int{}}}
	}

	tokens := computeSemanticTokens(source, full.Program)
	encoded := encodeSemanticTokens(tokens)

	return rpcMessage{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result:  map[string]any{"data": encoded},
	}
}

// computeSemanticTokens walks the IR and source to extract semantic tokens.
func computeSemanticTokens(source []byte, prog *ir.Program) []semanticToken {
	if prog == nil {
		return nil
	}

	lines := strings.Split(string(source), "\n")
	var tokens []semanticToken

	// Expert rule action targets (assert/emit/retract/modify).
	for _, er := range prog.Expert {
		if er.Target == "" {
			continue
		}
		tok := findTokenInSource(lines, er.Target, er.Span, "assert", "emit", "retract", "modify")
		if tok != nil {
			tok.tokenType = 1 // type
			tokens = append(tokens, *tok)
		}
	}

	// Lookup table references.
	for i := range prog.Exprs {
		expr := &prog.Exprs[i]
		if expr.Kind != ir.ExprLookup || expr.TableName == "" {
			continue
		}
		tok := findTokenInSource(lines, expr.TableName, expr.Span, "lookup")
		if tok != nil {
			tok.tokenType = 2 // struct
			tokens = append(tokens, *tok)
		}
	}

	// Member access chains (ExprVarRef with dots).
	for i := range prog.Exprs {
		expr := &prog.Exprs[i]
		if expr.Kind != ir.ExprVarRef || !strings.Contains(expr.Path, ".") {
			continue
		}
		tokens = append(tokens, memberAccessTokens(expr)...)
	}

	// Qualified names in requires/excludes for standard rules.
	for _, r := range prog.Rules {
		tokens = append(tokens, qualifiedPrereqTokens(lines, r.Prereqs, r.Span)...)
		tokens = append(tokens, qualifiedPrereqTokens(lines, r.Excludes, r.Span)...)
	}

	// Qualified names in requires/excludes for expert rules.
	for _, er := range prog.Expert {
		tokens = append(tokens, qualifiedPrereqTokens(lines, er.Prereqs, er.Span)...)
		tokens = append(tokens, qualifiedPrereqTokens(lines, er.Excludes, er.Span)...)
	}

	// Sort by position.
	sort.Slice(tokens, func(i, j int) bool {
		if tokens[i].line != tokens[j].line {
			return tokens[i].line < tokens[j].line
		}
		return tokens[i].col < tokens[j].col
	})

	return tokens
}

// findTokenInSource searches for a token name within a span, appearing after one
// of the given keyword prefixes. Returns the token's source position.
func findTokenInSource(lines []string, name string, span ir.Span, keywords ...string) *semanticToken {
	startRow := int(span.StartRow)
	endRow := int(span.EndRow)
	if endRow >= len(lines) {
		endRow = len(lines) - 1
	}

	for row := startRow; row <= endRow; row++ {
		if row < 0 || row >= len(lines) {
			continue
		}
		line := lines[row]
		for _, kw := range keywords {
			pattern := kw + " " + name
			idx := strings.Index(line, pattern)
			if idx < 0 {
				continue
			}
			col := idx + len(kw) + 1
			return &semanticToken{
				line:   row,
				col:    col,
				length: len(name),
			}
		}
	}
	return nil
}

// memberAccessTokens splits a dotted ExprVarRef path and emits property tokens
// for all fields after the first segment.
func memberAccessTokens(expr *ir.Expr) []semanticToken {
	parts := strings.Split(expr.Path, ".")
	if len(parts) < 2 {
		return nil
	}

	var tokens []semanticToken
	line := int(expr.Span.StartRow)
	col := int(expr.Span.StartCol)

	// Skip the first part (the variable itself).
	offset := len(parts[0]) + 1 // +1 for the dot

	for _, part := range parts[1:] {
		tokens = append(tokens, semanticToken{
			line:      line,
			col:       col + offset,
			length:    len(part),
			tokenType: 3, // property
		})
		offset += len(part) + 1 // +1 for next dot
	}
	return tokens
}

// qualifiedPrereqTokens emits namespace tokens for dotted prereq/excludes names.
func qualifiedPrereqTokens(lines []string, names []string, ruleSpan ir.Span) []semanticToken {
	var tokens []semanticToken
	for _, name := range names {
		dotIdx := strings.Index(name, ".")
		if dotIdx < 0 {
			continue
		}
		prefix := name[:dotIdx]

		// Search for the full qualified name within the rule's span.
		startRow := int(ruleSpan.StartRow)
		endRow := int(ruleSpan.EndRow)
		if endRow >= len(lines) {
			endRow = len(lines) - 1
		}

		for row := startRow; row <= endRow; row++ {
			if row < 0 || row >= len(lines) {
				continue
			}
			line := lines[row]
			idx := strings.Index(line, name)
			if idx < 0 {
				continue
			}
			// Emit namespace token for the prefix.
			tokens = append(tokens, semanticToken{
				line:      row,
				col:       idx,
				length:    len(prefix),
				tokenType: 0, // namespace
			})
			// Emit variable token for the suffix.
			tokens = append(tokens, semanticToken{
				line:      row,
				col:       idx + dotIdx + 1,
				length:    len(name) - dotIdx - 1,
				tokenType: 4, // variable
			})
			break
		}
	}
	return tokens
}

// encodeSemanticTokens converts absolute-position tokens into the LSP
// delta-encoded []int format. Tokens must already be sorted by position.
func encodeSemanticTokens(tokens []semanticToken) []int {
	if len(tokens) == 0 {
		return []int{}
	}

	data := make([]int, 0, len(tokens)*5)
	prevLine := 0
	prevCol := 0

	for _, tok := range tokens {
		deltaLine := tok.line - prevLine
		deltaCol := tok.col
		if deltaLine == 0 {
			deltaCol = tok.col - prevCol
		}

		data = append(data,
			deltaLine,
			deltaCol,
			tok.length,
			tok.tokenType,
			tok.modifiers,
		)

		prevLine = tok.line
		prevCol = tok.col
	}

	return data
}
