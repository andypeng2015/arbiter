package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func positionForSubstring(t *testing.T, content, needle string) (int, int) {
	t.Helper()
	offset := strings.Index(content, needle)
	if offset < 0 {
		t.Fatalf("missing substring %q", needle)
	}
	line := strings.Count(content[:offset], "\n")
	col := offset - strings.LastIndex(content[:offset], "\n") - 1
	if line == 0 {
		col = offset
	}
	return line, col
}

func TestCompileWorkspaceDiagnosticsImportedModuleErrors(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.arb")
	scoringPath := filepath.Join(dir, "scoring.arb")

	mainContent := `import "scoring"
rule Main {
	when { true }
	then Allow {}
}
`
	scoringContent := `tag "fraud"
rule Broken tag "frad" {
	when { true }
	then Alert {}
}
`
	if err := os.WriteFile(mainPath, []byte(mainContent), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	if err := os.WriteFile(scoringPath, []byte(scoringContent), 0o644); err != nil {
		t.Fatalf("write scoring: %v", err)
	}

	files := map[string]*fileState{
		fileURI(mainPath):    {uri: fileURI(mainPath), content: mainContent},
		fileURI(scoringPath): {uri: fileURI(scoringPath), content: scoringContent},
	}
	diags := compileWorkspaceDiagnostics(files)

	mainDiags := diags[fileURI(mainPath)]
	if len(mainDiags) == 0 || !strings.Contains(mainDiags[0]["message"].(string), `imported module "scoring" has errors`) {
		t.Fatalf("expected import diagnostic in main, got %+v", mainDiags)
	}
	scoringDiags := diags[fileURI(scoringPath)]
	if len(scoringDiags) == 0 || !strings.Contains(scoringDiags[0]["message"].(string), `unknown tag "frad"`) {
		t.Fatalf("expected unknown tag diagnostic in scoring, got %+v", scoringDiags)
	}
}

func TestComputeCodeActionsAddRequires(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.arb")
	scoringPath := filepath.Join(dir, "scoring.arb")

	mainContent := `import "scoring"
rule Main {
	when { scoring.BaseRule }
	then Allow {}
}
`
	scoringContent := `rule BaseRule {
	when { true }
	then Allow {}
}
`
	if err := os.WriteFile(mainPath, []byte(mainContent), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	if err := os.WriteFile(scoringPath, []byte(scoringContent), 0o644); err != nil {
		t.Fatalf("write scoring: %v", err)
	}

	line, char := positionForSubstring(t, mainContent, "scoring.BaseRule")
	params := codeActionParams{}
	params.TextDocument.URI = fileURI(mainPath)
	params.Range.Start.Line = line
	params.Range.Start.Character = char + len("scoring.")
	params.Range.End = params.Range.Start

	actions := computeCodeActions(params.TextDocument.URI, mainContent, map[string]*fileState{
		fileURI(mainPath):    {uri: fileURI(mainPath), content: mainContent},
		fileURI(scoringPath): {uri: fileURI(scoringPath), content: scoringContent},
	}, params)

	found := false
	for _, action := range actions {
		if action["title"] == "Add requires scoring.BaseRule" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected requires code action, got %+v", actions)
	}
}

func TestComputeCodeActionsImportQuickFix(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.arb")
	scoringPath := filepath.Join(dir, "scoring.arb")

	mainContent := `rule Main {
	when { scoring.BaseRule }
	then Allow {}
}
`
	scoringContent := `rule BaseRule {
	when { true }
	then Allow {}
}
`
	if err := os.WriteFile(mainPath, []byte(mainContent), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	if err := os.WriteFile(scoringPath, []byte(scoringContent), 0o644); err != nil {
		t.Fatalf("write scoring: %v", err)
	}

	line, char := positionForSubstring(t, mainContent, "scoring.BaseRule")
	params := codeActionParams{}
	params.TextDocument.URI = fileURI(mainPath)
	params.Range.Start.Line = line
	params.Range.Start.Character = char + len("scoring.")
	params.Range.End = params.Range.Start

	actions := computeCodeActions(params.TextDocument.URI, mainContent, map[string]*fileState{
		fileURI(mainPath): {uri: fileURI(mainPath), content: mainContent},
	}, params)

	found := false
	for _, action := range actions {
		if action["title"] == `Import "scoring"` {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected import quick fix, got %+v", actions)
	}
}

func TestComputeCodeActionsLookupElseQuickFix(t *testing.T) {
	content := `table limits {
	score: number | verdict: string
	1 | "review"
}

rule Main {
	when { true }
	then Allow {
		let row = lookup limits where score > 0
		result: row.verdict,
	}
}
`
	line, char := positionForSubstring(t, content, "lookup limits")
	params := codeActionParams{}
	params.TextDocument.URI = "untitled:main.arb"
	params.Range.Start.Line = line
	params.Range.Start.Character = char + len("lookup ")
	params.Range.End = params.Range.Start
	params.Context.Diagnostics = []struct {
		Message string `json:"message"`
	}{
		{Message: "lookup without else may return null"},
	}

	actions := computeCodeActions(params.TextDocument.URI, content, map[string]*fileState{
		params.TextDocument.URI: {uri: params.TextDocument.URI, content: content},
	}, params)

	found := false
	for _, action := range actions {
		if action["title"] == "Add else clause to lookup" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected lookup quick fix, got %+v", actions)
	}
}

func TestComputeCodeActionsMissingOutcomeFieldQuickFix(t *testing.T) {
	content := `outcome Profile {
	name: string
	preset: string
}

rule Main {
	when { true }
	then Profile {
		name: "ok",
	}
}
`
	line, char := positionForSubstring(t, content, "name: \"ok\"")
	params := codeActionParams{}
	params.TextDocument.URI = "untitled:main.arb"
	params.Range.Start.Line = line
	params.Range.Start.Character = char
	params.Range.End = params.Range.Start
	params.Context.Diagnostics = []struct {
		Message string `json:"message"`
	}{
		{Message: `rule Main action Profile: missing required field "preset"`},
	}

	actions := computeCodeActions(params.TextDocument.URI, content, map[string]*fileState{
		params.TextDocument.URI: {uri: params.TextDocument.URI, content: content},
	}, params)

	found := false
	for _, action := range actions {
		if action["title"] == `Add missing field "preset"` {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected missing-field quick fix, got %+v", actions)
	}
}
