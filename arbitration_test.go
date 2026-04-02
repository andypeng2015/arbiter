package arbiter_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/flags"
	"github.com/odvcencio/arbiter/govern"
)

func TestArbitraceCloneCopiesSteps(t *testing.T) {
	original := arbiter.Arbitrace{
		Steps: []arbiter.ArbitraceStep{
			govern.NewArbitraceStep("segment beta_users", true, "beta_users -> true"),
		},
	}

	clone := original.Clone()
	if !reflect.DeepEqual(clone, original) {
		t.Fatalf("clone = %#v, want %#v", clone, original)
	}

	clone.Steps[0].Detail = "changed"
	if original.Steps[0].Detail != "beta_users -> true" {
		t.Fatalf("clone mutated original arbitrace")
	}
}

func TestFlagEvaluationJSONUsesArbitraceField(t *testing.T) {
	value := flags.FlagEvaluation{
		Flag: "checkout_v2",
		Arbitrace: []flags.ArbitraceStep{
			govern.NewArbitraceStep("segment beta_users", true, "beta_users -> true"),
		},
	}

	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"arbitrace"`) {
		t.Fatalf("expected arbitrace field in JSON, got %s", text)
	}
	if strings.Contains(text, `"trace"`) {
		t.Fatalf("expected trace field to be gone from JSON, got %s", text)
	}
}

func TestArbitraceDispositionDefaultsAndDeferral(t *testing.T) {
	passed := govern.NewArbitraceStep("segment beta_users", true, "beta_users -> true")
	if passed.Disposition != govern.ArbitraceDispositionPassed {
		t.Fatalf("expected passed disposition, got %+v", passed)
	}

	blocked := govern.NewArbitraceStep("segment beta_users", false, "beta_users -> false")
	if blocked.Disposition != govern.ArbitraceDispositionBlocked {
		t.Fatalf("expected blocked disposition, got %+v", blocked)
	}

	deferred := govern.NewDeferredScopedArbitraceStep(
		govern.ArbitracePhaseGovernance,
		govern.ArbitraceScopeRule,
		"First",
		govern.ArbitraceKindExcludes,
		"Second",
		"",
		"Second not yet evaluated",
	)
	if deferred.Result {
		t.Fatalf("expected deferred result to remain false, got %+v", deferred)
	}
	if deferred.Disposition != govern.ArbitraceDispositionDeferred {
		t.Fatalf("expected deferred disposition, got %+v", deferred)
	}
}
