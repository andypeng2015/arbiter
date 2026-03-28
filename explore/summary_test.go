package explore_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/arbiter/explore"
	"github.com/odvcencio/arbiter/ir"
)

func TestBuildSummaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "greenhouse.arb")
	source := []byte(`
const SAFE_TEMP = 28 C

fact SensorReading {
	temperature: number<temperature>
}

outcome HeatWarning {
	zone: string
}

strategy RouteHeat returns HeatWarning {
	when { input.hot == true } kill_switch off then AlertNow {
		zone: "zone-a",
	}

	else Ignore {
		zone: "zone-b",
	}
}

worker notify_ops {
	input HeatWarning
	output HeatWarning
	webhook https://hooks.internal/heat
}

arbiter greenhouse {
	poll 30s
	source gsheet://greenhouse/readings
	source worker://notify_ops
	on HeatWarning worker notify_ops
}

rule CheckTemp {
	kill_switch on
	when { sensor.temperature > SAFE_TEMP }
	then Alert {}
}

expert rule HeatStress cooldown 15m {
	kill_switch off
	when { input.hot == true } for 10m
	then emit HeatWarning {
		zone: "zone-a",
	}
}
`)
	if err := os.WriteFile(path, source, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	summary, err := explore.BuildSummaryFile(path)
	if err != nil {
		t.Fatalf("BuildSummaryFile: %v", err)
	}
	if summary.Source != path {
		t.Fatalf("summary.Source = %q, want %q", summary.Source, path)
	}
	if len(summary.FactSchemas) != 1 || summary.FactSchemas[0].Name != "SensorReading" {
		t.Fatalf("unexpected fact schemas: %+v", summary.FactSchemas)
	}
	if len(summary.OutcomeSchemas) != 1 || summary.OutcomeSchemas[0].Name != "HeatWarning" {
		t.Fatalf("unexpected outcome schemas: %+v", summary.OutcomeSchemas)
	}
	if len(summary.Strategies) != 1 || summary.Strategies[0].Name != "RouteHeat" {
		t.Fatalf("unexpected strategies: %+v", summary.Strategies)
	}
	if len(summary.Workers) != 1 || summary.Workers[0].Name != "notify_ops" || summary.Workers[0].Kind != "webhook" {
		t.Fatalf("unexpected workers: %+v", summary.Workers)
	}
	if len(summary.Arbiters) != 1 || summary.Arbiters[0].Name != "greenhouse" {
		t.Fatalf("unexpected arbiters: %+v", summary.Arbiters)
	}
	if len(summary.Arbiters[0].Sources) != 2 || summary.Arbiters[0].Handlers[0].Kind != "worker" {
		t.Fatalf("unexpected arbiter summary: %+v", summary.Arbiters[0])
	}
	if len(summary.Constants) != 1 || summary.Constants[0].Raw != "28 C" {
		t.Fatalf("unexpected constants: %+v", summary.Constants)
	}
	if len(summary.Rules) != 1 || summary.Rules[0].Name != "CheckTemp" {
		t.Fatalf("unexpected rules: %+v", summary.Rules)
	}
	if summary.Rules[0].KillSwitch != ir.KillSwitchOn {
		t.Fatalf("expected rule kill_switch on, got %+v", summary.Rules[0])
	}
	if got := summary.Strategies[0].Candidates[0].KillSwitch; got != ir.KillSwitchOff {
		t.Fatalf("expected strategy candidate kill_switch off, got %+v", summary.Strategies[0].Candidates[0])
	}
	if len(summary.ExpertRules) != 1 {
		t.Fatalf("unexpected expert rules: %+v", summary.ExpertRules)
	}
	if summary.ExpertRules[0].KillSwitch != ir.KillSwitchOff {
		t.Fatalf("expected expert kill_switch off, got %+v", summary.ExpertRules[0])
	}
	if summary.ExpertRules[0].For != "10m" || summary.ExpertRules[0].Cooldown != "15m" {
		t.Fatalf("expected temporal metadata in expert summary, got %+v", summary.ExpertRules[0])
	}
	if len(summary.UsedUnits) == 0 {
		t.Fatalf("expected used units in summary, got %+v", summary)
	}
}

func TestBuildSummaryIncludesDecimalUnits(t *testing.T) {
	summary := explore.BuildSummary(&ir.Program{
		FactSchemas: []ir.FactSchema{{
			Name: "Transaction",
			Fields: []ir.SchemaField{{
				Name:     "amount",
				Type:     ir.FieldType{Base: "decimal", Dimension: "currency"},
				Required: true,
			}},
		}},
		Exprs: []ir.Expr{{
			Kind:   ir.ExprDecimalLit,
			String: "1000.25",
			Unit:   "USD",
		}},
	})

	found := false
	for _, dimension := range summary.UsedUnits {
		if dimension.Dimension == "currency" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected currency units in summary, got %+v", summary.UsedUnits)
	}
}
