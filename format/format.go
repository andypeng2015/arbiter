// Package format implements canonical formatting for Arbiter .arb files.
//
// The formatter normalizes indentation, spacing, and blank lines by processing
// lines of text. It does not parse a CST or AST; instead it uses a simple
// line-based approach similar to gofmt: correct enough to be useful, fast
// enough to run on every save.
//
// Canonical rules:
//   - 4-space indentation (tabs converted to spaces)
//   - Opening brace on same line as declaration
//   - One blank line between top-level declarations
//   - No trailing whitespace
//   - Trailing newline at end of file
//   - Consistent spacing around operators
//   - Param assignments use "key: value," (space after colon)
//   - Comments preserved in place
package format

import (
	"regexp"
	"strings"
)

// Format applies canonical formatting to Arbiter .arb source.
// On any internal error it returns the original source unchanged.
func Format(src []byte) []byte {
	if len(src) == 0 {
		return src
	}

	text := string(src)

	// Normalize line endings to \n.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")

	// Pass 1: normalize indentation (tabs -> 4 spaces, then re-indent).
	lines = normalizeIndentation(lines)

	// Pass 2: normalize spacing around operators and colons.
	lines = normalizeSpacing(lines)

	// Pass 3: trim trailing whitespace per line.
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}

	// Pass 4: ensure one blank line between top-level declarations,
	// and collapse multiple blank lines.
	lines = normalizeBlankLines(lines)

	// Join and ensure trailing newline.
	result := strings.Join(lines, "\n")
	result = strings.TrimRight(result, "\n") + "\n"

	return []byte(result)
}

// indentUnit is the canonical indent string.
const indentUnit = "    " // 4 spaces

// normalizeIndentation replaces any existing indentation with canonical
// 4-space indentation at the correct nesting depth. It tracks brace and
// parenthesis depth to determine the correct indent level.
func normalizeIndentation(lines []string) []string {
	out := make([]string, len(lines))

	depth := 0
	for i, line := range lines {
		stripped := stripIndent(line)

		// Blank lines stay empty.
		if stripped == "" {
			out[i] = ""
			continue
		}

		// Count opening and closing delimiters on this line.
		opens, closes := delimiterCount(stripped)

		// The indent for this line: decrease by the number of closing
		// delimiters that appear before any opening ones.
		indentDepth := depth
		leadingCloses := countLeadingCloses(stripped)
		indentDepth -= leadingCloses
		if indentDepth < 0 {
			indentDepth = 0
		}

		out[i] = strings.Repeat(indentUnit, indentDepth) + stripped

		// Update depth for the next line.
		depth += opens - closes
		if depth < 0 {
			depth = 0
		}
	}

	return out
}

// stripIndent removes all leading whitespace (tabs and spaces) from a line,
// converting tabs to 4 spaces first for consistency.
func stripIndent(line string) string {
	// Replace tabs with 4 spaces for measurement, then trim.
	return strings.TrimLeft(line, " \t")
}

// delimiterCount returns the number of opening and closing delimiters
// ({ and () on a line, ignoring those inside string literals and comments.
// Parentheses only count when they appear at the end of a line (opening)
// or start of a line (closing), indicating a multi-line grouping expression.
func delimiterCount(line string) (opens, closes int) {
	inString := false
	var strChar byte

	for i := 0; i < len(line); i++ {
		c := line[i]

		// Line comment -- stop scanning.
		if !inString && c == '#' {
			break
		}

		// Handle string boundaries.
		if !inString && (c == '"' || c == '\'') {
			inString = true
			strChar = c
			continue
		}
		if inString && c == strChar {
			escaped := false
			j := i - 1
			for j >= 0 && line[j] == '\\' {
				escaped = !escaped
				j--
			}
			if !escaped {
				inString = false
			}
			continue
		}

		if !inString {
			switch c {
			case '{':
				opens++
			case '}':
				closes++
			case '(':
				// Only count if it's a trailing open paren (end-of-line grouping).
				if isTrailingDelimiter(line, i) {
					opens++
				}
			case ')':
				// Only count if it's a leading close paren.
				if isLeadingDelimiter(line, i) {
					closes++
				}
			}
		}
	}
	return
}

// isTrailingDelimiter returns true if the character at pos is the last
// non-whitespace/non-comment character on the line.
func isTrailingDelimiter(line string, pos int) bool {
	for j := pos + 1; j < len(line); j++ {
		c := line[j]
		if c == ' ' || c == '\t' {
			continue
		}
		if c == '#' {
			return true
		}
		return false
	}
	return true
}

// isLeadingDelimiter returns true if the character at pos is the first
// non-whitespace character on the line.
func isLeadingDelimiter(line string, pos int) bool {
	for j := 0; j < pos; j++ {
		c := line[j]
		if c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}

// countLeadingCloses counts the number of closing delimiters ('}' or ')')
// at the start of a line (after stripping indent), before any opening
// delimiter. This determines how many levels to de-indent for this line.
// Only ')' that appears as the first non-whitespace char counts (matching
// the trailing-paren heuristic).
func countLeadingCloses(stripped string) int {
	count := 0
	for i := 0; i < len(stripped); i++ {
		c := stripped[i]
		if c == '}' {
			count++
		} else if c == ')' {
			// Only count leading ) as a de-indent if it's the very first
			// non-whitespace character.
			if i == 0 || allWhitespace(stripped[:i]) {
				count++
			} else {
				break
			}
		} else if c == '{' || c == '(' {
			break
		} else if c != ' ' && c != '\t' {
			break
		}
	}
	return count
}

// allWhitespace returns true if s contains only spaces and tabs.
func allWhitespace(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return false
		}
	}
	return true
}

// operatorRe matches comparison and arithmetic operators that should have
// spaces around them. Carefully avoids matching inside strings or comments.
var operatorRe = regexp.MustCompile(`([^\s><!:=])([><!]=?|==|!=)([^\s=])`)

// colonAssignRe matches "key:value" param assignments (no space after colon)
// but not URIs like "chain://".
var colonAssignRe = regexp.MustCompile(`^(\s*\w+):(\S)`)

// normalizeSpacing fixes operator spacing and colon spacing in param
// assignments. It operates line-by-line and skips comment lines.
func normalizeSpacing(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = normalizeLineSpacing(line)
	}
	return out
}

// normalizeLineSpacing normalizes spacing for a single line.
func normalizeLineSpacing(line string) string {
	stripped := strings.TrimLeft(line, " \t")

	// Skip blank lines and full-line comments.
	if stripped == "" || strings.HasPrefix(stripped, "#") {
		return line
	}

	indent := line[:len(line)-len(stripped)]

	// Split into code and comment portions.
	code, comment := splitComment(stripped)

	// Normalize operator spacing in the code portion.
	code = normalizeOperators(code)

	// Normalize colon spacing in param assignments: "key: value".
	code = normalizeColonSpacing(code)

	result := indent + code
	if comment != "" {
		result += comment
	}
	return result
}

// splitComment splits a line into code and comment parts, preserving the
// comment prefix. Returns (code, comment) where comment includes the leading
// space and # character, or is empty if there's no comment.
func splitComment(line string) (string, string) {
	inString := false
	var strChar byte

	for i := 0; i < len(line); i++ {
		c := line[i]

		if !inString && c == '#' {
			code := line[:i]
			comment := line[i:]
			return code, comment
		}

		if !inString && (c == '"' || c == '\'') {
			inString = true
			strChar = c
			continue
		}
		if inString && c == strChar {
			escaped := false
			j := i - 1
			for j >= 0 && line[j] == '\\' {
				escaped = !escaped
				j--
			}
			if !escaped {
				inString = false
			}
			continue
		}
	}
	return line, ""
}

// normalizeOperators ensures spaces around comparison operators like >,<,>=,<=,==,!=.
// It avoids breaking strings and handles edge cases like negative numbers.
func normalizeOperators(code string) string {
	// Process character-by-character to avoid modifying strings.
	var out strings.Builder
	out.Grow(len(code) + 16)

	inString := false
	var strChar byte

	for i := 0; i < len(code); i++ {
		c := code[i]

		// String handling.
		if !inString && (c == '"' || c == '\'') {
			inString = true
			strChar = c
			out.WriteByte(c)
			continue
		}
		if inString {
			out.WriteByte(c)
			if c == strChar {
				escaped := false
				j := i - 1
				for j >= 0 && code[j] == '\\' {
					escaped = !escaped
					j--
				}
				if !escaped {
					inString = false
				}
			}
			continue
		}

		// Check for two-character operators: >=, <=, ==, !=
		if i+1 < len(code) {
			pair := code[i : i+2]
			if pair == ">=" || pair == "<=" || pair == "==" || pair == "!=" {
				ensureSpaceBefore(&out)
				out.WriteString(pair)
				i++
				// Ensure space after.
				if i+1 < len(code) && code[i+1] != ' ' && code[i+1] != '\t' {
					out.WriteByte(' ')
				}
				continue
			}
		}

		// Single-character operators: >, <
		// But not inside compound constructs like ->, =>, or type annotations
		// like decimal<currency>.
		if c == '>' || c == '<' {
			prev := prevNonSpace(&out)
			// Skip if preceded by another operator character (part of -> or =>).
			if prev == '-' || prev == '=' {
				out.WriteByte(c)
				continue
			}
			// Detect type annotations: identifier<identifier> pattern.
			// If < follows an identifier char directly (no space) and the
			// content up to > looks like a type parameter, don't add spaces.
			if c == '<' && isIdentByte(prev) {
				// Look ahead for closing > to check for type annotation.
				if isTypeAnnotation(code, i) {
					out.WriteByte(c)
					continue
				}
			}
			if c == '>' && isTypeAnnotationClose(code, i) {
				out.WriteByte(c)
				continue
			}
			ensureSpaceBefore(&out)
			out.WriteByte(c)
			// Ensure space after, but not before '='.
			if i+1 < len(code) && code[i+1] != ' ' && code[i+1] != '\t' && code[i+1] != '=' {
				out.WriteByte(' ')
			}
			continue
		}

		out.WriteByte(c)
	}

	return out.String()
}

// ensureSpaceBefore adds a space before the current write position if the
// last character written is not already a space or the builder is empty.
func ensureSpaceBefore(b *strings.Builder) {
	s := b.String()
	if len(s) > 0 && s[len(s)-1] != ' ' && s[len(s)-1] != '\t' {
		b.WriteByte(' ')
	}
}

// prevNonSpace returns the last non-space character in the builder, or 0.
func prevNonSpace(b *strings.Builder) byte {
	s := b.String()
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] != ' ' && s[i] != '\t' {
			return s[i]
		}
	}
	return 0
}

// isIdentByte reports whether b is a valid identifier character.
func isIdentByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') || b == '_'
}

// isTypeAnnotation checks if code[pos] is a '<' that starts a type
// annotation like decimal<currency>. It looks for identifier<identifier>
// with no spaces inside the angle brackets.
func isTypeAnnotation(code string, pos int) bool {
	// Must have an identifier character immediately before.
	if pos == 0 || !isIdentByte(code[pos-1]) {
		return false
	}
	// Scan forward for matching '>'.
	for j := pos + 1; j < len(code); j++ {
		c := code[j]
		if c == '>' {
			// Valid type annotation if there's at least one char between < and >.
			return j > pos+1
		}
		if !isIdentByte(c) {
			// Non-identifier character inside — not a type annotation.
			return false
		}
	}
	return false
}

// isTypeAnnotationClose checks if code[pos] is a '>' that closes a type
// annotation like decimal<currency>.
func isTypeAnnotationClose(code string, pos int) bool {
	// Scan backwards for matching '<' with only identifier chars between.
	for j := pos - 1; j >= 0; j-- {
		c := code[j]
		if c == '<' {
			// Must have identifier char before the '<'.
			return j > 0 && isIdentByte(code[j-1])
		}
		if !isIdentByte(c) {
			return false
		}
	}
	return false
}

// normalizeColonSpacing ensures "key: value" has exactly one space after
// the colon in param assignments. Handles "key:value" -> "key: value" but
// avoids touching URIs like "chain://".
func normalizeColonSpacing(code string) string {
	if !colonAssignRe.MatchString(code) {
		return code
	}
	return colonAssignRe.ReplaceAllString(code, "${1}: ${2}")
}

// topLevelDeclPrefixes are the keywords that start top-level declarations.
var topLevelDeclPrefixes = []string{
	"rule ", "expert ", "segment ", "flag ", "const ", "feature ",
	"fact ", "outcome ", "strategy ", "worker ", "arbiter ", "include ",
}

// isTopLevelDecl returns true if the line (after stripping indent) begins
// a new top-level declaration.
func isTopLevelDecl(line string) bool {
	return topLevelDeclKind(line) != ""
}

// topLevelDeclKind returns the declaration keyword if the line starts a
// top-level declaration, or "" otherwise.
func topLevelDeclKind(line string) string {
	stripped := strings.TrimLeft(line, " \t")
	for _, prefix := range topLevelDeclPrefixes {
		if strings.HasPrefix(stripped, prefix) {
			return strings.TrimSpace(prefix)
		}
	}
	return ""
}

// groupableDeclKinds are declaration kinds that form logical groups:
// consecutive declarations of the same kind don't need blank line separation.
var groupableDeclKinds = map[string]bool{
	"const":   true,
	"include": true,
}

// isComment returns true if the line is a comment (possibly with leading whitespace).
func isComment(line string) bool {
	stripped := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(stripped, "#")
}

// normalizeBlankLines ensures exactly one blank line between top-level
// declarations and collapses runs of multiple blank lines.
func normalizeBlankLines(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}

	var out []string

	for i, line := range lines {
		// Collapse consecutive blank lines into at most one.
		if strings.TrimSpace(line) == "" {
			if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
				continue
			}
			out = append(out, "")
			continue
		}

		// Before a top-level declaration, ensure exactly one blank line
		// separates it from the previous non-blank content (unless it's
		// the very first line, or preceded by a comment block that belongs to it).
		kind := topLevelDeclKind(line)
		if kind != "" && len(out) > 0 {
			lastNonBlank := lastNonBlankLine(out)

			// Skip blank-line insertion between consecutive groupable
			// declarations of the same kind (e.g., const, include).
			prevKind := ""
			if lastNonBlank >= 0 {
				prevKind = topLevelDeclKind(out[lastNonBlank])
			}
			sameGroup := groupableDeclKinds[kind] && kind == prevKind

			if !sameGroup {
				if lastNonBlank >= 0 && !isComment(out[lastNonBlank]) {
					// Ensure blank line before this declaration.
					if strings.TrimSpace(out[len(out)-1]) != "" {
						out = append(out, "")
					}
				} else if lastNonBlank >= 0 && isComment(out[lastNonBlank]) {
					// The previous line is a comment. Check if there's a blank
					// line before the comment block -- if so, the comment belongs
					// to this declaration and we're good. If not, we should ensure
					// a blank line before the comment block.
					commentStart := lastNonBlank
					for commentStart > 0 && isComment(out[commentStart-1]) {
						commentStart--
					}
					if commentStart > 0 && strings.TrimSpace(out[commentStart-1]) != "" {
						// Insert blank line before the comment block.
						rest := make([]string, len(out)-commentStart)
						copy(rest, out[commentStart:])
						out = append(out[:commentStart], "")
						out = append(out, rest...)
					}
				}
			}
		}

		// Before a comment that's followed by a top-level decl and is at
		// top level, we handled it above. Just append.
		_ = i
		out = append(out, line)
	}

	// Remove leading blank lines.
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}

	// Remove trailing blank lines (the final \n is added by the caller).
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}

	return out
}

// lastNonBlankLine returns the index of the last non-blank line in out,
// or -1 if all lines are blank.
func lastNonBlankLine(out []string) int {
	for i := len(out) - 1; i >= 0; i-- {
		if strings.TrimSpace(out[i]) != "" {
			return i
		}
	}
	return -1
}
