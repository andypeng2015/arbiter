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

const scaffoldManifest = `[project]
name = "%s"
version = "0.1.0"
`

const scaffoldLib = "# Shared limits, imported by other modules as `limits.*`.\n" + `
const MinAge = 18
const MaxRiskScore = 80
`

const scaffoldMain = `# Entry module. Imports shared limits from lib/limits.arb.
#
#   arbiter check main.arb --strict
#   arbiter eval  main.arb --data '{"user":{"age":21},"risk":{"score":40}}'

import "lib/limits"

input {
    user: {
        age: number
    }
    risk: {
        score: number
    }
}

outcome Access {
    tier: string
}

rule Eligible {
    when { user.age >= limits.MinAge and risk.score <= limits.MaxRiskScore }
    then Access { tier: "full" }
}
`

// runInit scaffolds a starter rules.arb + rules.test.arb in the target directory
// (default "."). It refuses to overwrite existing files. The scaffold is written
// to pass `arbiter check --strict` and `arbiter test` out of the box.
func runInit(args []string) error {
	dir := "."
	modular := false
	for _, a := range args {
		if a == "--module" {
			modular = true
			continue
		}
		if !strings.HasPrefix(a, "-") {
			dir = a
		}
	}
	if modular {
		return initModule(dir)
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

// initModule scaffolds a multi-file modular project: an arbiter.toml manifest,
// a shared lib/limits.arb defining reusable constants, and a main.arb that
// imports them by namespace. It demonstrates the import/reuse surface and is
// written to pass `arbiter check --strict` out of the box.
func initModule(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	name := filepath.Base(dir)
	if abs, err := filepath.Abs(dir); err == nil {
		name = filepath.Base(abs)
	}

	files := []struct{ name, content string }{
		{"arbiter.toml", fmt.Sprintf(scaffoldManifest, name)},
		{filepath.Join("lib", "limits.arb"), scaffoldLib},
		{"main.arb", scaffoldMain},
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
	fmt.Fprintf(os.Stderr, "\nnext:\n  arbiter check %s --strict\n",
		filepath.Join(dir, "main.arb"))
	return nil
}
