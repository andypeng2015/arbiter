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
				"completionProvider":   map[string]any{"triggerCharacters": []string{".", " "}},
				"hoverProvider":        true,
				"definitionProvider":   true,
				"diagnosticProvider":   map[string]any{"interFileDependencies": true, "workspaceDiagnostics": false},
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
	// Clear diagnostics.
	return []rpcMessage{s.diagnosticNotification(params.TextDocument.URI, nil)}
}

// --- Diagnostics ---

func (s *server) publishDiagnostics(uri string) []rpcMessage {
	s.mu.Lock()
	f, ok := s.files[uri]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	content := f.content
	s.mu.Unlock()

	diags := compileDiagnostics(content)
	return []rpcMessage{s.diagnosticNotification(uri, diags)}
}

func compileDiagnostics(source string) []map[string]any {
	_, err := compileSource([]byte(source))
	if err == nil {
		return nil
	}

	var diags []map[string]any
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		d := parseDiagnosticLine(line)
		diags = append(diags, d)
	}
	return diags
}

func parseDiagnosticLine(line string) map[string]any {
	// Try to parse "file:line:col: message" or "line:col: message"
	lineNum, col, message := 0, 0, line

	parts := strings.SplitN(line, ":", 4)
	if len(parts) >= 3 {
		if n, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
			lineNum = n
			if c, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
				col = c
				message = strings.TrimSpace(parts[2])
			}
		} else if len(parts) >= 4 {
			if n, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
				lineNum = n
				if c, err := strconv.Atoi(strings.TrimSpace(parts[2])); err == nil {
					col = c
					message = strings.TrimSpace(parts[3])
				}
			}
		}
	}

	if lineNum > 0 {
		lineNum-- // LSP is 0-based
	}
	if col > 0 {
		col-- // LSP is 0-based
	}

	return map[string]any{
		"range": map[string]any{
			"start": map[string]any{"line": lineNum, "character": col},
			"end":   map[string]any{"line": lineNum, "character": col + 1},
		},
		"severity": 1, // Error
		"source":   "arbiter",
		"message":  message,
	}
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
		"any", "all", "none", "in", "and", "or", "not",
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
	// TODO: Implement go-to-definition by tracking declaration positions in IR.
	return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: nil}
}

// --- Helpers ---

func compileSource(source []byte) (any, error) {
	_, err := compileForLSP(source)
	return nil, err
}

func compileForLSP(source []byte) (*lspCompileResult, error) {
	full, err := fullCompile(source)
	if err != nil {
		return nil, err
	}
	return &lspCompileResult{full: full}, nil
}

type lspCompileResult struct {
	full any
}

func fullCompile(source []byte) (any, error) {
	// Import arbiter at function level to avoid circular import issues.
	// The LSP binary links against the arbiter package.
	return nil, compileAndValidate(source)
}

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
