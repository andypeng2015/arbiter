package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/ir"
	"github.com/odvcencio/arbiter/units"
	gotreesitter "github.com/odvcencio/gotreesitter"
)

type codeActionParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	Range struct {
		Start struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"start"`
		End struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"end"`
	} `json:"range"`
	Context struct {
		Diagnostics []struct {
			Message string `json:"message"`
		} `json:"diagnostics"`
	} `json:"context"`
}

type textIndex struct {
	lineStarts []int
}

type analysisDoc struct {
	uri         string
	path        string
	content     string
	index       textIndex
	projectRoot string
	overlay     map[string]string
	resolver    overlayModuleResolver
	imports     []importSite
	parsed      *arbiter.ParsedSource
	program     *ir.Program
	moduleCache map[string]*ir.Program
}

func (s *server) handleCodeAction(msg rpcMessage) rpcMessage {
	var params codeActionParams
	json.Unmarshal(msg.Params, &params)

	s.mu.Lock()
	files := make(map[string]*fileState, len(s.files))
	for key, value := range s.files {
		copyState := *value
		files[key] = &copyState
	}
	current := files[params.TextDocument.URI]
	s.mu.Unlock()

	if current == nil {
		return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: []any{}}
	}

	actions := computeCodeActions(params.TextDocument.URI, current.content, files, params)
	return rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: actions}
}

func computeCodeActions(uri, content string, files map[string]*fileState, params codeActionParams) []map[string]any {
	doc := analyzeDocument(uri, content, files)
	if doc == nil {
		return nil
	}

	line := params.Range.Start.Line
	char := params.Range.Start.Character
	var actions []map[string]any

	for _, diag := range params.Context.Diagnostics {
		if action := missingOutcomeFieldAction(doc, line, char, diag.Message); action != nil {
			actions = append(actions, action)
		}
		if action := lookupElseAction(doc, line, char, diag.Message); action != nil {
			actions = append(actions, action)
		}
	}

	if action := addRequiresAction(doc, line, char); action != nil {
		actions = append(actions, action)
	}
	actions = append(actions, importQuickFixActions(doc, line, char)...)
	return dedupeActions(actions)
}

func analyzeDocument(uri, content string, files map[string]*fileState) *analysisDoc {
	doc := &analysisDoc{
		uri:         uri,
		content:     content,
		index:       buildTextIndex(content),
		overlay:     snapshotOverlays(files),
		moduleCache: make(map[string]*ir.Program),
	}
	if path, ok := uriToPath(uri); ok {
		doc.path = path
		doc.projectRoot = projectRoot(path)
		doc.resolver = overlayModuleResolver{root: doc.projectRoot, overlays: doc.overlay}
		doc.imports = extractImportSites(path, content, doc.resolver)
	}
	parsed, err := arbiter.ParseSource([]byte(content))
	if err != nil {
		return doc
	}
	program, err := ir.Lower(parsed.Root, parsed.Source, parsed.Lang)
	if err != nil {
		return doc
	}
	doc.parsed = parsed
	doc.program = program
	return doc
}

func missingOutcomeFieldAction(doc *analysisDoc, line, char int, message string) map[string]any {
	fieldName, ok := missingFieldName(message)
	if !ok || doc.program == nil || doc.parsed == nil {
		return nil
	}
	thenBlock := smallestNodeContaining(doc.parsed.Root, doc.parsed.Lang, doc.index.offset(line, char), "then_block")
	if thenBlock == nil {
		return nil
	}
	actionNameNode := thenBlock.ChildByFieldName("action_name", doc.parsed.Lang)
	if actionNameNode == nil {
		return nil
	}
	actionName := sourceText(actionNameNode, []byte(doc.content))
	schema, ok := doc.program.OutcomeSchemaByName(actionName)
	if !ok || schema == nil {
		return nil
	}
	var field *ir.SchemaField
	for i := range schema.Fields {
		if schema.Fields[i].Name == fieldName {
			field = &schema.Fields[i]
			break
		}
	}
	if field == nil {
		return nil
	}
	closeLine, closeChar := doc.index.byteToPosition(int(thenBlock.EndByte()) - 1)
	blockIndent := lineIndent(doc.content, closeLine)
	newText := blockIndent + indentUnit(doc.content) + field.Name + ": " + zeroValueText(field.Type) + ",\n"
	return workspaceEditAction(
		fmt.Sprintf("Add missing field %q", field.Name),
		"quickfix",
		doc.uri,
		closeLine,
		closeChar,
		newText,
	)
}

func lookupElseAction(doc *analysisDoc, line, char int, message string) map[string]any {
	if !strings.Contains(message, "lookup without else may return null") || doc.program == nil || doc.parsed == nil {
		return nil
	}
	lookup := smallestNodeContaining(doc.parsed.Root, doc.parsed.Lang, doc.index.offset(line, char), "lookup_expr")
	if lookup == nil {
		return nil
	}
	if lookup.ChildByFieldName("else", doc.parsed.Lang) != nil {
		return nil
	}
	tableNode := lookup.ChildByFieldName("table", doc.parsed.Lang)
	if tableNode == nil {
		return nil
	}
	tableName := sourceText(tableNode, []byte(doc.content))
	table, ok := doc.program.TableByName(tableName)
	if !ok || table == nil || len(table.Columns) == 0 {
		return nil
	}
	parts := make([]string, 0, len(table.Columns))
	for _, col := range table.Columns {
		parts = append(parts, fmt.Sprintf("%s: %s", col.Name, zeroValueText(col.Type)))
	}
	insertLine, insertChar := doc.index.byteToPosition(int(lookup.EndByte()))
	return workspaceEditAction(
		"Add else clause to lookup",
		"quickfix",
		doc.uri,
		insertLine,
		insertChar,
		" else { "+strings.Join(parts, ", ")+" }",
	)
}

func addRequiresAction(doc *analysisDoc, line, char int) map[string]any {
	qualified, ok := qualifiedNameAtPosition(doc.content, line, char)
	if !ok || !doc.importedRuleOrFlag(qualified) || doc.parsed == nil {
		return nil
	}
	offset := doc.index.offset(line, char)
	decl := smallestNodeContaining(doc.parsed.Root, doc.parsed.Lang, offset, "rule_declaration", "expert_rule_declaration")
	if decl == nil {
		return nil
	}
	whenNode := decl.ChildByFieldName("condition", doc.parsed.Lang)
	if whenNode == nil || offset < int(whenNode.StartByte()) || offset > int(whenNode.EndByte()) {
		return nil
	}
	if declarationHasRequires(decl, doc.parsed.Lang, []byte(doc.content), qualified) {
		return nil
	}
	insertLine, insertChar := doc.index.byteToPosition(int(whenNode.StartByte()))
	indent := lineIndent(doc.content, insertLine)
	return workspaceEditAction(
		fmt.Sprintf("Add requires %s", qualified),
		"refactor.rewrite",
		doc.uri,
		insertLine,
		insertChar,
		indent+"requires "+qualified+"\n",
	)
}

func importQuickFixActions(doc *analysisDoc, line, char int) []map[string]any {
	qualified, ok := qualifiedNameAtPosition(doc.content, line, char)
	if !ok || doc.path == "" {
		return nil
	}
	parts := strings.SplitN(qualified, ".", 2)
	if len(parts) != 2 {
		return nil
	}
	qualifier := parts[0]
	target := parts[1]
	for _, site := range doc.imports {
		if site.Alias == qualifier {
			return nil
		}
	}
	candidates := doc.findImportCandidates(qualifier, target)
	if len(candidates) == 0 {
		return nil
	}
	insertLine := 0
	if len(doc.imports) > 0 {
		last := doc.imports[0]
		for _, site := range doc.imports[1:] {
			if site.Line > last.Line {
				last = site
			}
		}
		insertLine = last.Line + 1
	}
	var actions []map[string]any
	for _, candidate := range candidates {
		actions = append(actions, workspaceEditAction(
			fmt.Sprintf("Import %q", candidate),
			"quickfix",
			doc.uri,
			insertLine,
			0,
			fmt.Sprintf("import %q\n", candidate),
		))
	}
	return actions
}

func (d *analysisDoc) importedRuleOrFlag(qualified string) bool {
	if d.path == "" {
		return false
	}
	parts := strings.SplitN(qualified, ".", 2)
	if len(parts) != 2 {
		return false
	}
	qualifier, target := parts[0], parts[1]
	for _, site := range d.imports {
		if site.Alias != qualifier || site.ResolvedPath == "" || site.ResolveErr != nil {
			continue
		}
		module := d.loadModule(site.ResolvedPath)
		if module == nil {
			return false
		}
		if _, ok := module.RuleByName(target); ok {
			return true
		}
		if _, ok := module.FlagByName(target); ok {
			return true
		}
		return false
	}
	return false
}

func (d *analysisDoc) findImportCandidates(qualifier, target string) []string {
	root := d.projectRoot
	if root == "" {
		root = filepath.Dir(d.path)
	}
	var candidates []string
	seen := make(map[string]struct{})
	filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		if filepath.Base(path) != qualifier+".arb" {
			return nil
		}
		module := d.loadModule(path)
		if module == nil {
			return nil
		}
		if _, ok := module.RuleByName(target); !ok {
			if _, ok := module.FlagByName(target); !ok {
				return nil
			}
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		importPath := filepath.ToSlash(strings.TrimSuffix(rel, ".arb"))
		if _, ok := seen[importPath]; ok {
			return nil
		}
		seen[importPath] = struct{}{}
		candidates = append(candidates, importPath)
		return nil
	})
	sort.Strings(candidates)
	return candidates
}

func (d *analysisDoc) loadModule(path string) *ir.Program {
	path = filepath.Clean(path)
	if module, ok := d.moduleCache[path]; ok {
		return module
	}
	content, ok := d.overlay[path]
	if !ok {
		bytes, err := os.ReadFile(path)
		if err != nil {
			d.moduleCache[path] = nil
			return nil
		}
		content = string(bytes)
	}
	parsed, err := arbiter.ParseSource([]byte(content))
	if err != nil {
		d.moduleCache[path] = nil
		return nil
	}
	module, err := ir.Lower(parsed.Root, parsed.Source, parsed.Lang)
	if err != nil {
		d.moduleCache[path] = nil
		return nil
	}
	d.moduleCache[path] = module
	return module
}

func declarationHasRequires(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, qualified string) bool {
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child.Type(lang) != "rule_requires" {
			continue
		}
		nameNode := child.ChildByFieldName("name", lang)
		if nameNode == nil {
			continue
		}
		if sourceText(nameNode, source) == qualified {
			return true
		}
	}
	return false
}

func smallestNodeContaining(root *gotreesitter.Node, lang *gotreesitter.Language, offset int, types ...string) *gotreesitter.Node {
	if root == nil || offset < 0 {
		return nil
	}
	typeSet := make(map[string]struct{}, len(types))
	for _, typ := range types {
		typeSet[typ] = struct{}{}
	}
	var best *gotreesitter.Node
	var visit func(*gotreesitter.Node)
	visit = func(node *gotreesitter.Node) {
		if node == nil {
			return
		}
		start := int(node.StartByte())
		end := int(node.EndByte())
		if offset < start || offset > end {
			return
		}
		if _, ok := typeSet[node.Type(lang)]; ok {
			if best == nil || (end-start) < int(best.EndByte()-best.StartByte()) {
				best = node
			}
		}
		for i := 0; i < int(node.NamedChildCount()); i++ {
			visit(node.NamedChild(i))
		}
	}
	visit(root)
	return best
}

func missingFieldName(message string) (string, bool) {
	const marker = `missing required field "`
	idx := strings.Index(message, marker)
	if idx < 0 {
		return "", false
	}
	rest := message[idx+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

func qualifiedNameAtPosition(content string, line, char int) (string, bool) {
	lines := strings.Split(content, "\n")
	if line < 0 || line >= len(lines) {
		return "", false
	}
	text := lines[line]
	if len(text) == 0 {
		return "", false
	}
	if char >= len(text) {
		char = len(text) - 1
	}
	if char < 0 {
		return "", false
	}
	start, end := char, char
	for start > 0 && isQualifiedChar(text[start-1]) {
		start--
	}
	for end < len(text) && isQualifiedChar(text[end]) {
		end++
	}
	word := text[start:end]
	if strings.Count(word, ".") != 1 {
		return "", false
	}
	parts := strings.SplitN(word, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	if !isIdentifier(parts[0]) || !isIdentifier(parts[1]) {
		return "", false
	}
	return word, true
}

func isQualifiedChar(ch byte) bool {
	return isIdentChar(ch) || ch == '.'
}

func isIdentifier(text string) bool {
	if text == "" {
		return false
	}
	for i := range len(text) {
		if !isIdentChar(text[i]) {
			return false
		}
	}
	return true
}

func workspaceEditAction(title, kind, uri string, line, char int, newText string) map[string]any {
	return map[string]any{
		"title": title,
		"kind":  kind,
		"edit": map[string]any{
			"changes": map[string]any{
				uri: []map[string]any{{
					"range": map[string]any{
						"start": map[string]any{"line": line, "character": char},
						"end":   map[string]any{"line": line, "character": char},
					},
					"newText": newText,
				}},
			},
		},
	}
}

func buildTextIndex(content string) textIndex {
	starts := []int{0}
	for i, ch := range content {
		if ch == '\n' {
			starts = append(starts, i+1)
		}
	}
	return textIndex{lineStarts: starts}
}

func (t textIndex) offset(line, char int) int {
	if len(t.lineStarts) == 0 {
		return 0
	}
	if line < 0 {
		return 0
	}
	if line >= len(t.lineStarts) {
		return len(t.lineStarts) - 1
	}
	return t.lineStarts[line] + max(0, char)
}

func (t textIndex) byteToPosition(offset int) (int, int) {
	if offset < 0 {
		return 0, 0
	}
	line := sort.Search(len(t.lineStarts), func(i int) bool { return t.lineStarts[i] > offset }) - 1
	if line < 0 {
		line = 0
	}
	return line, offset - t.lineStarts[line]
}

func lineIndent(content string, line int) string {
	lines := strings.Split(content, "\n")
	if line < 0 || line >= len(lines) {
		return ""
	}
	text := lines[line]
	i := 0
	for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
		i++
	}
	return text[:i]
}

func indentUnit(content string) string {
	if strings.Contains(content, "\t") {
		return "\t"
	}
	return "    "
}

func zeroValueText(fieldType ir.FieldType) string {
	switch fieldType.Base {
	case "string":
		return `""`
	case "bool", "boolean":
		return "false"
	case "number", "decimal":
		if fieldType.Dimension == "" {
			return "0"
		}
		if units.KnownDimension(fieldType.Dimension) {
			symbols := units.SymbolsForDimension(fieldType.Dimension)
			if len(symbols) > 0 {
				return "0 " + symbols[0]
			}
		}
		return "0 " + fieldType.Dimension
	case "timestamp":
		return `"1970-01-01T00:00:00Z"`
	default:
		return "null"
	}
}

func dedupeActions(actions []map[string]any) []map[string]any {
	if len(actions) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(actions))
	seen := make(map[string]struct{}, len(actions))
	for _, action := range actions {
		title, _ := action["title"].(string)
		if title == "" {
			continue
		}
		if _, ok := seen[title]; ok {
			continue
		}
		seen[title] = struct{}{}
		out = append(out, action)
	}
	return out
}

func sourceText(node *gotreesitter.Node, source []byte) string {
	return string(source[node.StartByte():node.EndByte()])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
