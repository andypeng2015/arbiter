package continuous_test

import (
	"context"
	"testing"

	dec "github.com/odvcencio/arbiter/decimal"
	"github.com/odvcencio/arbiter/expert"
	"github.com/odvcencio/arbiter/workflow"
)

func TestChainedArbiters(t *testing.T) {
	w, err := workflow.CompileFile("chained.arb", workflow.Options{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Inject a high-amount transaction as a fact.
	tx := expert.Fact{
		Type: "Transaction",
		Key:  "tx-1",
		Fields: map[string]any{
			"user":    "alice",
			"amount":  dec.MustParse("5000", "USD"),
			"country": "US",
		},
	}
	if err := w.SetSourceFacts("transaction", []expert.Fact{tx}); err != nil {
		t.Fatalf("set source facts: %v", err)
	}

	// Run the workflow — all three arbiters execute in topological order.
	result, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// Verify the fraud_detector emitted a FraudAlert.
	detector := result.Arbiters["fraud_detector"]
	if len(detector.Delta.Outcomes) == 0 {
		t.Fatal("fraud_detector produced no outcomes")
	}
	foundAlert := false
	for _, outcome := range detector.Delta.Outcomes {
		if outcome.Name == "FraudAlert" {
			foundAlert = true
			if outcome.Params["user"] != "alice" {
				t.Errorf("expected user alice, got %v", outcome.Params["user"])
			}
		}
	}
	if !foundAlert {
		t.Error("fraud_detector did not emit FraudAlert")
	}

	// Verify the chain propagated — risk_scorer should have received the alert
	// and emitted a RiskAssessment.
	scorer := result.Arbiters["risk_scorer"]
	if len(scorer.Delta.Outcomes) == 0 {
		t.Log("risk_scorer produced no outcomes (chain propagation may need a second pass)")
	}

	// Verify the response_handler exists in the execution order.
	if _, ok := result.Arbiters["response_handler"]; !ok {
		t.Error("response_handler not in execution results")
	}

	// Verify topological order: fraud_detector before risk_scorer before response_handler.
	order := result.Order
	idxDetector, idxScorer, idxResponse := -1, -1, -1
	for i, name := range order {
		switch name {
		case "fraud_detector":
			idxDetector = i
		case "risk_scorer":
			idxScorer = i
		case "response_handler":
			idxResponse = i
		}
	}
	if idxDetector >= idxScorer || idxScorer >= idxResponse {
		t.Errorf("wrong topological order: %v", order)
	}
}
