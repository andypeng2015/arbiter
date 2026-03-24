package format

import (
	"testing"
)

func TestNormalizeIndentation(t *testing.T) {
	input := `rule Foo priority 1 {
	when {
		user.age > 18
	}
	then Allow {
		reason: "adult",
	}
}`
	want := `rule Foo priority 1 {
    when {
        user.age > 18
    }
    then Allow {
        reason: "adult",
    }
}
`
	got := string(Format([]byte(input)))
	if got != want {
		t.Errorf("tab indentation not normalized\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestMixedIndentation(t *testing.T) {
	// 2-space indent mixed with tabs.
	input := "rule Bar priority 2 {\n  when {\n\t\tuser.active == true\n  }\n  then Match {\n    reason: \"ok\",\n  }\n}\n"
	want := "rule Bar priority 2 {\n    when {\n        user.active == true\n    }\n    then Match {\n        reason: \"ok\",\n    }\n}\n"
	got := string(Format([]byte(input)))
	if got != want {
		t.Errorf("mixed indentation not normalized\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestBlankLinesBetweenDeclarations(t *testing.T) {
	input := `rule A priority 1 {
    when {
        true
    }
    then Allow {
        reason: "a",
    }
}
rule B priority 2 {
    when {
        true
    }
    then Allow {
        reason: "b",
    }
}`
	want := `rule A priority 1 {
    when {
        true
    }
    then Allow {
        reason: "a",
    }
}

rule B priority 2 {
    when {
        true
    }
    then Allow {
        reason: "b",
    }
}
`
	got := string(Format([]byte(input)))
	if got != want {
		t.Errorf("missing blank line between declarations not added\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestTrailingWhitespace(t *testing.T) {
	input := "rule X priority 1 {   \n    when {  \n        true\n    }\n    then Allow {\n        reason: \"ok\",   \n    }  \n}\n"
	got := string(Format([]byte(input)))
	for i, line := range splitLines(got) {
		// Last element after split may be empty due to trailing newline.
		if line == "" && i == len(splitLines(got))-1 {
			continue
		}
		if line != trimRight(line) {
			t.Errorf("line %d has trailing whitespace: %q", i+1, line)
		}
	}
}

func TestTrailingNewline(t *testing.T) {
	input := "rule X priority 1 {\n    when {\n        true\n    }\n    then Allow {\n        reason: \"ok\",\n    }\n}"
	got := string(Format([]byte(input)))
	if got[len(got)-1] != '\n' {
		t.Error("result does not end with newline")
	}
	// Should not end with multiple newlines.
	if len(got) > 1 && got[len(got)-2] == '\n' {
		t.Error("result ends with multiple newlines")
	}
}

func TestIdempotent(t *testing.T) {
	canonical := `# Fraud detection rules

segment high_risk {
    tx.amount > 1000
}

rule Block priority 0 {
    when segment high_risk {
        account.flagged == true
    }
    then Block {
        reason: "flagged",
        escalate: "ops",
    }
}

rule Allow priority 99 {
    when {
        true
    }
    then Allow {
        reason: "default",
    }
}
`
	got := string(Format([]byte(canonical)))
	if got != canonical {
		t.Errorf("formatted canonical input changed\ngot:\n%s\nwant:\n%s", got, canonical)
	}

	// Apply format again — must be stable.
	got2 := string(Format([]byte(got)))
	if got2 != got {
		t.Errorf("second format pass changed output\nfirst:\n%s\nsecond:\n%s", got, got2)
	}
}

func TestOperatorSpacing(t *testing.T) {
	input := `rule X priority 1 {
    when {
        user.age>18
        and user.score>=100
        and user.level!=0
        and user.rank<=5
        and user.id==42
    }
    then Allow {
        reason: "ok",
    }
}
`
	got := string(Format([]byte(input)))
	want := `rule X priority 1 {
    when {
        user.age > 18
        and user.score >= 100
        and user.level != 0
        and user.rank <= 5
        and user.id == 42
    }
    then Allow {
        reason: "ok",
    }
}
`
	if got != want {
		t.Errorf("operator spacing not normalized\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestColonSpacing(t *testing.T) {
	input := `rule X priority 1 {
    when {
        true
    }
    then Allow {
        reason:"ok",
        count:42,
    }
}
`
	got := string(Format([]byte(input)))
	want := `rule X priority 1 {
    when {
        true
    }
    then Allow {
        reason: "ok",
        count: 42,
    }
}
`
	if got != want {
		t.Errorf("colon spacing not normalized\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestCommentsPreserved(t *testing.T) {
	input := `# Top-level comment

# Rule doc
rule X priority 1 {
    when {
        true # inline comment
    }
    then Allow {
        reason: "ok",
    }
}
`
	got := string(Format([]byte(input)))
	if got != input {
		t.Errorf("comments not preserved\ngot:\n%s\nwant:\n%s", got, input)
	}
}

func TestMultipleBlankLinesCollapsed(t *testing.T) {
	input := "rule A priority 1 {\n    when {\n        true\n    }\n    then Allow {\n        reason: \"a\",\n    }\n}\n\n\n\nrule B priority 2 {\n    when {\n        true\n    }\n    then Allow {\n        reason: \"b\",\n    }\n}\n"
	got := string(Format([]byte(input)))
	want := "rule A priority 1 {\n    when {\n        true\n    }\n    then Allow {\n        reason: \"a\",\n    }\n}\n\nrule B priority 2 {\n    when {\n        true\n    }\n    then Allow {\n        reason: \"b\",\n    }\n}\n"
	if got != want {
		t.Errorf("multiple blank lines not collapsed\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestEmptyInput(t *testing.T) {
	got := Format([]byte(""))
	if string(got) != "" {
		t.Errorf("empty input should produce empty output, got: %q", got)
	}
}

func TestCommentBeforeDeclaration(t *testing.T) {
	input := `rule A priority 1 {
    when {
        true
    }
    then Allow {
        reason: "a",
    }
}
# Doc for B
rule B priority 2 {
    when {
        true
    }
    then Allow {
        reason: "b",
    }
}
`
	want := `rule A priority 1 {
    when {
        true
    }
    then Allow {
        reason: "a",
    }
}

# Doc for B
rule B priority 2 {
    when {
        true
    }
    then Allow {
        reason: "b",
    }
}
`
	got := string(Format([]byte(input)))
	if got != want {
		t.Errorf("blank line not added before doc comment\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestConstDeclarations(t *testing.T) {
	// Consecutive consts should stay grouped (no blank line between them),
	// but a blank line should separate the const block from other decls.
	input := `const A = 1
const B = 2
rule X priority 1 {
    when {
        true
    }
    then Allow {
        reason: "ok",
    }
}
`
	want := `const A = 1
const B = 2

rule X priority 1 {
    when {
        true
    }
    then Allow {
        reason: "ok",
    }
}
`
	got := string(Format([]byte(input)))
	if got != want {
		t.Errorf("const declarations not grouped\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestWindowsLineEndings(t *testing.T) {
	input := "rule X priority 1 {\r\n    when {\r\n        true\r\n    }\r\n    then Allow {\r\n        reason: \"ok\",\r\n    }\r\n}\r\n"
	got := string(Format([]byte(input)))
	// Should use unix line endings.
	if contains(got, "\r") {
		t.Error("output contains carriage return characters")
	}
	// Should still be valid.
	want := "rule X priority 1 {\n    when {\n        true\n    }\n    then Allow {\n        reason: \"ok\",\n    }\n}\n"
	if got != want {
		t.Errorf("windows line endings not normalized\ngot:\n%q\nwant:\n%q", got, want)
	}
}

func TestStringContentNotModified(t *testing.T) {
	// Operators inside strings should not be modified.
	input := `rule X priority 1 {
    when {
        user.name == "a>b"
    }
    then Allow {
        reason: "score>=100",
    }
}
`
	got := string(Format([]byte(input)))
	if got != input {
		t.Errorf("string content was modified\ngot:\n%s\nwant:\n%s", got, input)
	}
}

func TestFormatTableAlignment(t *testing.T) {
	input := "table t {\nheight: number|bitrate: string|preset: string\n1080|\"6500k\"|\"p3\"\n720|\"3800k\"|\"p3\"\n}\n"
	expected := "table t {\n    height: number | bitrate: string | preset: string\n    1080           | \"6500k\"         | \"p3\"\n    720            | \"3800k\"         | \"p3\"\n}\n"
	got := string(Format([]byte(input)))
	if got != expected {
		t.Fatalf("table alignment:\ngot:\n%s\nexpected:\n%s", got, expected)
	}
}

// helpers

func splitLines(s string) []string {
	return split(s, "\n")
}

func split(s, sep string) []string {
	var parts []string
	for {
		i := indexOf(s, sep)
		if i < 0 {
			parts = append(parts, s)
			return parts
		}
		parts = append(parts, s[:i])
		s = s[i+len(sep):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trimRight(s string) string {
	i := len(s)
	for i > 0 && (s[i-1] == ' ' || s[i-1] == '\t') {
		i--
	}
	return s[:i]
}

func contains(s, sub string) bool {
	return indexOf(s, sub) >= 0
}
