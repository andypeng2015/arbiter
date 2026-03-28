// Command arbiter-lsp implements a Language Server Protocol server for Arbiter
// .arb files. It provides diagnostics, go-to-definition, hover, and completions.
//
// Usage:
//
//	arbiter-lsp (runs on stdin/stdout, launched by the editor)
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/odvcencio/arbiter/format"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetPrefix("[arbiter-lsp] ")

	s := &server{
		files: make(map[string]*fileState),
	}
	s.run(os.Stdin, os.Stdout)
}

type server struct {
	mu    sync.Mutex
	files map[string]*fileState
}

type fileState struct {
	uri     string
	content string
	version int
}

// --- JSON-RPC 2.0 ---

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *server) run(in io.Reader, out io.Writer) {
	reader := bufio.NewReader(in)
	writer := bufio.NewWriter(out)

	for {
		msg, err := readMessage(reader)
		if err != nil {
			if err == io.EOF {
				return
			}
			log.Printf("read error: %v", err)
			return
		}

		responses := s.handle(msg)
		for _, resp := range responses {
			if err := writeMessage(writer, resp); err != nil {
				log.Printf("write error: %v", err)
				return
			}
		}
		writer.Flush()
	}
}

func (s *server) handle(msg rpcMessage) []rpcMessage {
	switch msg.Method {
	case "initialize":
		return []rpcMessage{s.handleInitialize(msg)}
	case "initialized":
		return nil
	case "shutdown":
		return []rpcMessage{{JSONRPC: "2.0", ID: msg.ID, Result: nil}}
	case "exit":
		os.Exit(0)
		return nil
	case "textDocument/didOpen":
		return s.handleDidOpen(msg)
	case "textDocument/didChange":
		return s.handleDidChange(msg)
	case "textDocument/didSave":
		return s.handleDidSave(msg)
	case "textDocument/didClose":
		return s.handleDidClose(msg)
	case "textDocument/completion":
		return []rpcMessage{s.handleCompletion(msg)}
	case "textDocument/hover":
		return []rpcMessage{s.handleHover(msg)}
	case "textDocument/definition":
		return []rpcMessage{s.handleDefinition(msg)}
	case "textDocument/references":
		return []rpcMessage{s.handleReferences(msg)}
	case "textDocument/rename":
		return []rpcMessage{s.handleRename(msg)}
	case "textDocument/documentSymbol":
		return []rpcMessage{s.handleDocumentSymbol(msg)}
	case "textDocument/formatting":
		return []rpcMessage{s.handleFormatting(msg)}
	case "textDocument/codeAction":
		return []rpcMessage{s.handleCodeAction(msg)}
	case "textDocument/semanticTokens/full":
		return []rpcMessage{s.handleSemanticTokens(msg)}
	default:
		if msg.ID != nil {
			return []rpcMessage{{
				JSONRPC: "2.0",
				ID:      msg.ID,
				Error:   &rpcError{Code: -32601, Message: "method not found: " + msg.Method},
			}}
		}
		return nil
	}
}

// --- Initialize ---

func (s *server) handleInitialize(msg rpcMessage) rpcMessage {
	return rpcMessage{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result: map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync": map[string]any{
					"openClose": true,
					"change":    1, // Full sync
					"save":      map[string]any{"includeText": true},
				},
				"completionProvider":         map[string]any{"triggerCharacters": []string{".", " "}},
				"hoverProvider":              true,
				"definitionProvider":         true,
				"referencesProvider":         true,
				"renameProvider":             true,
				"documentSymbolProvider":     true,
				"documentFormattingProvider": true,
				"codeActionProvider":         true,
				"semanticTokensProvider": map[string]any{
					"legend": map[string]any{
						"tokenTypes":     semanticTokenTypes,
						"tokenModifiers": semanticTokenModifiers,
					},
					"full": true,
				},
				"diagnosticProvider": map[string]any{"interFileDependencies": true, "workspaceDiagnostics": false},
			},
			"serverInfo": map[string]any{
				"name":    "arbiter-lsp",
				"version": "0.13.0",
			},
		},
	}
}

// --- Document Sync ---

type didOpenParams struct {
	TextDocument struct {
		URI     string `json:"uri"`
		Text    string `json:"text"`
		Version int    `json:"version"`
	} `json:"textDocument"`
}

type didChangeParams struct {
	TextDocument struct {
		URI     string `json:"uri"`
		Version int    `json:"version"`
	} `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

type didSaveParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Text string `json:"text"`
}

func (s *server) handleDidOpen(msg rpcMessage) []rpcMessage {
	var params didOpenParams
	json.Unmarshal(msg.Params, &params)
	s.mu.Lock()
	s.files[params.TextDocument.URI] = &fileState{
		uri:     params.TextDocument.URI,
		content: params.TextDocument.Text,
		version: params.TextDocument.Version,
	}
	s.mu.Unlock()
	return s.publishDiagnostics(params.TextDocument.URI)
}

func (s *server) handleDidChange(msg rpcMessage) []rpcMessage {
	var params didChangeParams
	json.Unmarshal(msg.Params, &params)
	s.mu.Lock()
	if f, ok := s.files[params.TextDocument.URI]; ok {
		if len(params.ContentChanges) > 0 {
			f.content = params.ContentChanges[len(params.ContentChanges)-1].Text
		}
		f.version = params.TextDocument.Version
	}
	s.mu.Unlock()
	return s.publishDiagnostics(params.TextDocument.URI)
}

func (s *server) handleDidSave(msg rpcMessage) []rpcMessage {
	var params didSaveParams
	json.Unmarshal(msg.Params, &params)
	if params.Text != "" {
		s.mu.Lock()
		if f, ok := s.files[params.TextDocument.URI]; ok {
			f.content = params.Text
		}
		s.mu.Unlock()
	}
	return s.publishDiagnostics(params.TextDocument.URI)
}

func (s *server) handleDidClose(msg rpcMessage) []rpcMessage {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	json.Unmarshal(msg.Params, &params)
	s.mu.Lock()
	delete(s.files, params.TextDocument.URI)
	s.mu.Unlock()
	return s.publishDiagnostics(params.TextDocument.URI)
}

// --- Diagnostics ---

func (s *server) publishDiagnostics(uri string) []rpcMessage {
	s.mu.Lock()
	files := make(map[string]*fileState, len(s.files))
	for key, value := range s.files {
		copyState := *value
		files[key] = &copyState
	}
	s.mu.Unlock()

	workspace := compileWorkspaceDiagnostics(files)
	var notifications []rpcMessage
	for key, diags := range workspace {
		notifications = append(notifications, s.diagnosticNotification(key, diags))
	}
	if _, ok := files[uri]; !ok && uri != "" {
		notifications = append(notifications, s.diagnosticNotification(uri, nil))
	}
	return notifications
}

func (s *server) diagnosticNotification(uri string, diags []map[string]any) rpcMessage {
	if diags == nil {
		diags = []map[string]any{}
	}
	return rpcMessage{
		JSONRPC: "2.0",
		Method:  "textDocument/publishDiagnostics",
		Params:  mustJSON(map[string]any{"uri": uri, "diagnostics": diags}),
	}
}

// --- Completion ---

type textDocumentPositionParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Position struct {
		Line      int `json:"line"`
		Character int `json:"character"`
	} `json:"position"`
}

func (s *server) handleCompletion(msg rpcMessage) rpcMessage {
	var params textDocumentPositionParams
	json.Unmarshal(msg.Params, &params)

	s.mu.Lock()
	f, ok := s.files[params.TextDocument.URI]
	if !ok {
		s.mu.Unlock()
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: []any{}}
	}
	content := f.content
	s.mu.Unlock()

	items := computeCompletions(content, params.Position.Line, params.Position.Character)
	return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: items}
}

func computeCompletions(source string, line, col int) []map[string]any {
	summary := getSummary([]byte(source))
	if summary == nil {
		return keywordCompletions()
	}

	var items []map[string]any

	// Offer fact types.
	for _, f := range summary.FactSchemas {
		items = append(items, map[string]any{
			"label":  f.Name,
			"kind":   22, // Struct
			"detail": "fact",
		})
	}
	// Offer outcome types.
	for _, o := range summary.OutcomeSchemas {
		items = append(items, map[string]any{
			"label":  o.Name,
			"kind":   22,
			"detail": "outcome",
		})
	}
	// Offer segment names from rules that reference them.
	seenSegments := make(map[string]bool)
	for _, r := range summary.Rules {
		if r.Segment != "" && !seenSegments[r.Segment] {
			seenSegments[r.Segment] = true
			items = append(items, map[string]any{
				"label":  r.Segment,
				"kind":   15, // Enum
				"detail": "segment",
			})
		}
	}
	// Offer strategy names.
	for _, strat := range summary.Strategies {
		items = append(items, map[string]any{
			"label":  strat.Name,
			"kind":   3, // Function
			"detail": "strategy returns " + strat.Returns,
		})
	}
	// Offer rule names (for requires/excludes).
	for _, r := range summary.Rules {
		items = append(items, map[string]any{
			"label":  r.Name,
			"kind":   12, // Value
			"detail": "rule → " + r.Action,
		})
	}
	for _, r := range summary.ExpertRules {
		items = append(items, map[string]any{
			"label":  r.Name,
			"kind":   12,
			"detail": "expert rule → " + r.Kind + " " + r.Target,
		})
	}

	items = append(items, keywordCompletions()...)
	return items
}

func keywordCompletions() []map[string]any {
	keywords := []string{
		"rule", "expert", "strategy", "flag", "segment", "const",
		"fact", "outcome", "feature", "worker", "arbiter", "include",
		"when", "then", "otherwise", "assert", "emit", "retract", "modify",
		"requires", "excludes", "kill_switch", "rollout", "priority",
		"any", "all", "none", "in", "and", "or", "not", "on", "off",
		"true", "false", "null",
	}
	var items []map[string]any
	for _, kw := range keywords {
		items = append(items, map[string]any{
			"label": kw,
			"kind":  14, // Keyword
		})
	}
	return items
}

// --- Hover ---

func (s *server) handleHover(msg rpcMessage) rpcMessage {
	var params textDocumentPositionParams
	json.Unmarshal(msg.Params, &params)

	s.mu.Lock()
	f, ok := s.files[params.TextDocument.URI]
	if !ok {
		s.mu.Unlock()
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: nil}
	}
	content := f.content
	s.mu.Unlock()

	word := wordAtPosition(content, params.Position.Line, params.Position.Character)
	if word == "" {
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: nil}
	}

	hover := computeHover([]byte(content), word)
	if hover == "" {
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: nil}
	}

	return rpcMessage{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result: map[string]any{
			"contents": map[string]any{
				"kind":  "markdown",
				"value": hover,
			},
		},
	}
}

func computeHover(source []byte, word string) string {
	summary := getSummary(source)
	if summary == nil {
		return ""
	}

	// Check facts.
	for _, f := range summary.FactSchemas {
		if f.Name == word {
			var fields []string
			for _, field := range f.Fields {
				fields = append(fields, fmt.Sprintf("  %s: %s", field.Name, field.Type))
			}
			return fmt.Sprintf("**fact** `%s`\n```\n%s\n```", f.Name, strings.Join(fields, "\n"))
		}
	}
	// Check outcomes.
	for _, o := range summary.OutcomeSchemas {
		if o.Name == word {
			var fields []string
			for _, field := range o.Fields {
				fields = append(fields, fmt.Sprintf("  %s: %s", field.Name, field.Type))
			}
			return fmt.Sprintf("**outcome** `%s`\n```\n%s\n```", o.Name, strings.Join(fields, "\n"))
		}
	}
	// Check rules.
	for _, r := range summary.Rules {
		if r.Name == word {
			return fmt.Sprintf("**rule** `%s` (priority %d) → `%s`", r.Name, r.Priority, r.Action)
		}
	}
	// Check expert rules.
	for _, r := range summary.ExpertRules {
		if r.Name == word {
			return fmt.Sprintf("**expert rule** `%s` (priority %d) → %s `%s`", r.Name, r.Priority, r.Kind, r.Target)
		}
	}
	// Check strategies.
	for _, strat := range summary.Strategies {
		if strat.Name == word {
			return fmt.Sprintf("**strategy** `%s` returns `%s`", strat.Name, strat.Returns)
		}
	}
	return ""
}

// --- Definition ---

func (s *server) handleDefinition(msg rpcMessage) rpcMessage {
	var params textDocumentPositionParams
	json.Unmarshal(msg.Params, &params)

	s.mu.Lock()
	f, ok := s.files[params.TextDocument.URI]
	if !ok {
		s.mu.Unlock()
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: nil}
	}
	content := f.content
	s.mu.Unlock()

	word := wordAtPosition(content, params.Position.Line, params.Position.Character)
	if word == "" {
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: nil}
	}

	span := findDefinition([]byte(content), word)
	if span == nil {
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: nil}
	}

	return rpcMessage{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result: map[string]any{
			"uri": params.TextDocument.URI,
			"range": map[string]any{
				"start": map[string]any{"line": span.StartRow, "character": span.StartCol},
				"end":   map[string]any{"line": span.EndRow, "character": span.EndCol},
			},
		},
	}
}

// --- References ---

func (s *server) handleReferences(msg rpcMessage) rpcMessage {
	var params textDocumentPositionParams
	json.Unmarshal(msg.Params, &params)

	s.mu.Lock()
	f, ok := s.files[params.TextDocument.URI]
	if !ok {
		s.mu.Unlock()
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: []any{}}
	}
	content := f.content
	s.mu.Unlock()

	word := wordAtPosition(content, params.Position.Line, params.Position.Character)
	if word == "" {
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: []any{}}
	}

	refs := findReferences([]byte(content), word)
	locations := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		locations = append(locations, map[string]any{
			"uri": params.TextDocument.URI,
			"range": map[string]any{
				"start": map[string]any{"line": ref.StartRow, "character": ref.StartCol},
				"end":   map[string]any{"line": ref.EndRow, "character": ref.EndCol},
			},
		})
	}
	return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: locations}
}

// --- Rename ---

func (s *server) handleRename(msg rpcMessage) rpcMessage {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"position"`
		NewName string `json:"newName"`
	}
	json.Unmarshal(msg.Params, &params)

	s.mu.Lock()
	f, ok := s.files[params.TextDocument.URI]
	if !ok {
		s.mu.Unlock()
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: nil}
	}
	content := f.content
	s.mu.Unlock()

	word := wordAtPosition(content, params.Position.Line, params.Position.Character)
	if word == "" {
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: nil}
	}

	// Find all occurrences of the word in the source for text-based rename.
	lines := strings.Split(content, "\n")
	var edits []map[string]any
	for lineIdx, line := range lines {
		start := 0
		for {
			idx := strings.Index(line[start:], word)
			if idx < 0 {
				break
			}
			pos := start + idx
			// Check word boundaries.
			if pos > 0 && isIdentChar(line[pos-1]) {
				start = pos + len(word)
				continue
			}
			end := pos + len(word)
			if end < len(line) && isIdentChar(line[end]) {
				start = end
				continue
			}
			edits = append(edits, map[string]any{
				"range": map[string]any{
					"start": map[string]any{"line": lineIdx, "character": pos},
					"end":   map[string]any{"line": lineIdx, "character": end},
				},
				"newText": params.NewName,
			})
			start = end
		}
	}

	if len(edits) == 0 {
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: nil}
	}

	return rpcMessage{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result: map[string]any{
			"changes": map[string]any{
				params.TextDocument.URI: edits,
			},
		},
	}
}

// --- Document Symbols ---

func (s *server) handleDocumentSymbol(msg rpcMessage) rpcMessage {
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
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: []any{}}
	}
	content := f.content
	s.mu.Unlock()

	syms := getSymbols([]byte(content))
	result := make([]map[string]any, 0, len(syms))
	for _, sym := range syms {
		kind := symbolKind(sym.kind)
		result = append(result, map[string]any{
			"name": sym.name,
			"kind": kind,
			"range": map[string]any{
				"start": map[string]any{"line": sym.span.StartRow, "character": sym.span.StartCol},
				"end":   map[string]any{"line": sym.span.EndRow, "character": sym.span.EndCol},
			},
			"selectionRange": map[string]any{
				"start": map[string]any{"line": sym.span.StartRow, "character": sym.span.StartCol},
				"end":   map[string]any{"line": sym.span.StartRow, "character": sym.span.StartCol + uint32(len(sym.name))},
			},
		})
	}
	return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: result}
}

func symbolKind(kind string) int {
	switch kind {
	case "rule", "expert":
		return 12 // Function
	case "fact", "outcome":
		return 23 // Struct
	case "segment":
		return 14 // Enum
	case "strategy":
		return 6 // Method
	case "const":
		return 14 // Constant
	case "flag":
		return 8 // Field
	case "worker":
		return 9 // Constructor
	case "arbiter":
		return 2 // Module
	case "table":
		return 16 // Array (closest LSP kind for a data table)
	default:
		return 1 // File
	}
}

// --- Formatting ---

func (s *server) handleFormatting(msg rpcMessage) rpcMessage {
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
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: []any{}}
	}
	content := f.content
	s.mu.Unlock()

	formatted := string(format.Format([]byte(content)))
	if formatted == content {
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: []any{}}
	}

	// Count lines in original document.
	lineCount := strings.Count(content, "\n")
	if len(content) > 0 && content[len(content)-1] != '\n' {
		lineCount++
	}

	// Return a single TextEdit that replaces the entire document.
	edits := []map[string]any{
		{
			"range": map[string]any{
				"start": map[string]any{"line": 0, "character": 0},
				"end":   map[string]any{"line": lineCount, "character": 0},
			},
			"newText": formatted,
		},
	}
	return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: edits}
}

// --- Helpers ---

func wordAtPosition(content string, line, col int) string {
	lines := strings.Split(content, "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	l := lines[line]
	if col < 0 || col >= len(l) {
		return ""
	}
	// Expand left and right to find word boundary.
	start, end := col, col
	for start > 0 && isIdentChar(l[start-1]) {
		start--
	}
	for end < len(l) && isIdentChar(l[end]) {
		end++
	}
	return l[start:end]
}

func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// --- LSP Transport ---

func readMessage(r *bufio.Reader) (rpcMessage, error) {
	// Read headers.
	contentLength := 0
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return rpcMessage{}, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:")))
			if err == nil {
				contentLength = n
			}
		}
	}
	if contentLength == 0 {
		return rpcMessage{}, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return rpcMessage{}, err
	}
	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return rpcMessage{}, err
	}
	return msg, nil
}

func writeMessage(w *bufio.Writer, msg rpcMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	if _, err := w.WriteString(header); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}
