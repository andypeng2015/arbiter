package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const scaffoldRules = `# Starter Arbiter ruleset.
#
#   arbiter check rules.arb --strict
#   arbiter test  rules.test.arb
#   arbiter eval  rules.arb --data '{"user":{"age":21,"country":"US"}}'

input {
    user: {
        age: number
        country: string
    }
}

outcome Access {
    tier: string
}

rule AdultUS {
    when { user.age >= 18 and user.country == "US" }
    then Access { tier: "full" }
}
`

const scaffoldTests = `# Tests for rules.arb. Run: arbiter test rules.test.arb

test "adult US user gets full access" {
    given {
        user.age: 21
        user.country: "US"
    }
    expect rule AdultUS matched
    expect action Access { tier: "full" }
}

test "minor does not match" {
    given {
        user.age: 15
        user.country: "US"
    }
    expect rule AdultUS not matched
}
`

// runInit scaffolds a starter rules.arb + rules.test.arb in the target directory
// (default "."). It refuses to overwrite existing files. The scaffold is written
// to pass `arbiter check --strict` and `arbiter test` out of the box.
func runInit(args []string) error {
	dir := "."
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			dir = a
			break
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	files := []struct{ name, content string }{
		{"rules.arb", scaffoldRules},
		{"rules.test.arb", scaffoldTests},
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, f.name)); err == nil {
			return fmt.Errorf("refusing to overwrite existing %s", filepath.Join(dir, f.name))
		}
	}
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		if err := os.WriteFile(p, []byte(f.content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
		fmt.Fprintf(os.Stderr, "created %s\n", p)
	}
	fmt.Fprintf(os.Stderr, "\nnext:\n  arbiter check %s --strict\n  arbiter test %s\n",
		filepath.Join(dir, "rules.arb"), filepath.Join(dir, "rules.test.arb"))
	return nil
}
