package arbiter

import (
	"testing"
)

func TestTableLookupEndToEnd(t *testing.T) {
	src := []byte(`
table ladder {
    height: number | bitrate: string
    1080 | "6500k"
    720 | "3800k"
    480 | "1200k"
}

rule Transcode {
    when { job.height >= 480 }
    then Profile {
        let row = lookup ladder where height <= job.height order by height desc else { height: 0, bitrate: "800k" }
        bitrate: row.bitrate,
    }
}
`)
	prog, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// height=900 -> should match 720 row (height<=900, sorted desc, first is 720)
	dc := DataFromMap(map[string]any{"job": map[string]any{"height": 900.0}}, prog)
	matched, err := Eval(prog, dc)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].Params["bitrate"] != "3800k" {
		t.Fatalf("expected bitrate=3800k, got %v", matched[0].Params["bitrate"])
	}
}

func TestTableLookupExact(t *testing.T) {
	src := []byte(`
table ladder {
    height: number | bitrate: string
    1080 | "6500k"
    720 | "3800k"
    480 | "1200k"
}

rule Transcode {
    when { job.height >= 480 }
    then Profile {
        let row = lookup ladder where height <= job.height order by height desc
        bitrate: row.bitrate,
    }
}
`)
	prog, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// height=1080 -> should match 1080 exactly
	dc := DataFromMap(map[string]any{"job": map[string]any{"height": 1080.0}}, prog)
	matched, err := Eval(prog, dc)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].Params["bitrate"] != "6500k" {
		t.Fatalf("expected bitrate=6500k, got %v", matched[0].Params["bitrate"])
	}
}

func TestTableLookupElse(t *testing.T) {
	src := []byte(`
table ladder {
    height: number | bitrate: string
    1080 | "6500k"
    720 | "3800k"
    480 | "1200k"
}

rule Transcode {
    when { job.height >= 100 }
    then Profile {
        let row = lookup ladder where height <= job.height order by height desc else { height: 0, bitrate: "800k" }
        bitrate: row.bitrate,
    }
}
`)
	prog, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// height=100 -> no row matches (all heights > 100) -> else row
	dc := DataFromMap(map[string]any{"job": map[string]any{"height": 100.0}}, prog)
	matched, err := Eval(prog, dc)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].Params["bitrate"] != "800k" {
		t.Fatalf("expected bitrate=800k from else, got %v", matched[0].Params["bitrate"])
	}
}

func TestTableLookupNullWithoutElse(t *testing.T) {
	src := []byte(`
table ladder {
    height: number | bitrate: string
    1080 | "6500k"
    720 | "3800k"
    480 | "1200k"
}

rule Transcode {
    when { job.height >= 100 }
    then Profile {
        let row = lookup ladder where height > 9999 else { height: 0, bitrate: "fallback" }
        bitrate: row.bitrate,
    }
}
`)
	prog, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// No match -> else row is used
	dc := DataFromMap(map[string]any{"job": map[string]any{"height": 500.0}}, prog)
	matched, err := Eval(prog, dc)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].Params["bitrate"] != "fallback" {
		t.Fatalf("expected bitrate=fallback from else, got %v", matched[0].Params["bitrate"])
	}
}

func TestTableLookupNullFieldAccess(t *testing.T) {
	// When lookup returns null (no else), field access on null returns null.
	src := []byte(`
table ladder {
    height: number | bitrate: string
    1080 | "6500k"
}

rule Transcode {
    when { job.height >= 100 }
    then Profile {
        let row = lookup ladder where height > 9999
        bitrate: row.bitrate,
    }
}
`)
	prog, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// No match, no else -> null in locals -> row.bitrate resolves to null
	dc := DataFromMap(map[string]any{"job": map[string]any{"height": 500.0}}, prog)
	matched, err := Eval(prog, dc)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].Params["bitrate"] != nil {
		t.Fatalf("expected bitrate=nil for null row, got %v", matched[0].Params["bitrate"])
	}
}

func TestTableLookupNoWhere(t *testing.T) {
	// Lookup without where clause returns the first row.
	src := []byte(`
table presets {
    name: string | value: number
    "default" | 42
    "other" | 99
}

rule GetPreset {
    when { input.ok == true }
    then Result {
        let row = lookup presets
        value: row.value,
    }
}
`)
	prog, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	dc := DataFromMap(map[string]any{"input": map[string]any{"ok": true}}, prog)
	matched, err := Eval(prog, dc)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].Params["value"] != 42.0 {
		t.Fatalf("expected value=42, got %v", matched[0].Params["value"])
	}
}

func TestTableLookupSortAsc(t *testing.T) {
	src := []byte(`
table ladder {
    height: number | bitrate: string
    1080 | "6500k"
    720 | "3800k"
    480 | "1200k"
}

rule Transcode {
    when { job.height >= 480 }
    then Profile {
        let row = lookup ladder where height <= job.height order by height asc
        bitrate: row.bitrate,
    }
}
`)
	prog, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// height=900, asc sort -> first match is 480
	dc := DataFromMap(map[string]any{"job": map[string]any{"height": 900.0}}, prog)
	matched, err := Eval(prog, dc)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].Params["bitrate"] != "1200k" {
		t.Fatalf("expected bitrate=1200k (asc), got %v", matched[0].Params["bitrate"])
	}
}

func TestTableLookupTwoColumnKey(t *testing.T) {
	src := []byte(`
table pricing {
    region: string | tier: string | rate: number
    "US" | "basic" | 9.99
    "US" | "pro" | 19.99
    "EU" | "basic" | 8.99
    "EU" | "pro" | 17.99
}

rule Price {
    when { order.region != "" }
    then ApplyRate {
        let row = lookup pricing where region == order.region and tier == order.tier
        rate: row.rate,
    }
}
`)
	prog, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	dc := DataFromMap(map[string]any{"order": map[string]any{"region": "EU", "tier": "pro"}}, prog)
	matched, err := Eval(prog, dc)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].Params["rate"] != 17.99 {
		t.Fatalf("expected rate=17.99, got %v", matched[0].Params["rate"])
	}
}
