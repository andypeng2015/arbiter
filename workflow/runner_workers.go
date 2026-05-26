package workflow

import (
	"fmt"
	"strings"
	"time"

	arbiter "m31labs.dev/arbiter"
	"m31labs.dev/arbiter/expert"
)

func (r *Runner) applyWorkerExecution(arbiterName string, worker arbiter.WorkerDeclaration, result WorkerExecution) error {
	facts, err := materializeWorkerFacts(arbiterName, worker, result)
	if err != nil {
		return err
	}
	state := r.sourceState(workerSourceTarget(worker.Name))
	if state == nil {
		return nil
	}
	now := r.now().UTC()
	r.mu.Lock()
	state.Available = true
	state.LastError = ""
	state.ConsecutiveFailures = 0
	state.LastAttemptAt = now
	state.LastSuccessAt = now
	state.NextRetryAt = time.Time{}
	state.FactCount = len(facts)
	state.lastFacts = cloneExpertFacts(facts)
	r.mu.Unlock()
	r.workflowMu.Lock()
	defer r.workflowMu.Unlock()
	return r.workflow.setRuntimeSourceFacts(workerSourceTarget(worker.Name), facts)
}

func (r *Runner) markWorkerSourceFailure(workerName string, now time.Time, err error) {
	if r == nil || err == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markWorkerSourceFailureLocked(workerName, now, err)
}

func (r *Runner) markWorkerSourceFailureLocked(workerName string, now time.Time, err error) {
	state := r.sources[workerSourceTarget(workerName)]
	if state == nil {
		return
	}
	state.Available = false
	state.LastError = err.Error()
	state.ConsecutiveFailures++
	state.LastAttemptAt = now
	state.FactCount = len(state.lastFacts)
}

func materializeWorkerFacts(arbiterName string, worker arbiter.WorkerDeclaration, result WorkerExecution) ([]expert.Fact, error) {
	if len(result.Facts) > 0 && len(result.Outcomes) > 0 {
		return nil, fmt.Errorf("worker %s: execution returned both facts and outcomes", worker.Name)
	}
	switch worker.OutputKind {
	case arbiter.WorkerOutputFact:
		if len(result.Outcomes) > 0 {
			return nil, fmt.Errorf("worker %s: expected fact output %s, got outcomes", worker.Name, worker.Output)
		}
		facts := cloneExpertFacts(result.Facts)
		for i := range facts {
			if strings.TrimSpace(facts[i].Type) == "" {
				facts[i].Type = worker.Output
			}
			if facts[i].Type != worker.Output {
				return nil, fmt.Errorf("worker %s: expected fact output %s, got %s", worker.Name, worker.Output, facts[i].Type)
			}
			if strings.TrimSpace(facts[i].Key) == "" {
				facts[i].Key = workerFactKey(worker.Name, facts[i])
			}
		}
		return facts, nil
	case arbiter.WorkerOutputOutcome:
		if len(result.Facts) > 0 {
			return nil, fmt.Errorf("worker %s: expected outcome output %s, got facts", worker.Name, worker.Output)
		}
		facts := make([]expert.Fact, 0, len(result.Outcomes))
		for _, outcome := range result.Outcomes {
			if strings.TrimSpace(outcome.Name) == "" {
				outcome.Name = worker.Output
			}
			if outcome.Name != worker.Output {
				return nil, fmt.Errorf("worker %s: expected outcome output %s, got %s", worker.Name, worker.Output, outcome.Name)
			}
			facts = append(facts, workerOutcomeFact(arbiterName, worker, outcome))
		}
		return facts, nil
	default:
		return nil, fmt.Errorf("worker %s: unsupported output kind %s", worker.Name, worker.OutputKind)
	}
}

func workerOutcomeFact(arbiterName string, worker arbiter.WorkerDeclaration, outcome expert.Outcome) expert.Fact {
	fields := cloneMap(outcome.Params)
	if fields == nil {
		fields = make(map[string]any, 3)
	}
	fields["source_arbiter"] = arbiterName
	fields["source_worker"] = worker.Name
	if outcome.Rule != "" {
		fields["source_rule"] = outcome.Rule
	}
	return expert.Fact{
		Type:   outcome.Name,
		Key:    outcomeFactKey("worker:"+worker.Name, outcome),
		Fields: fields,
	}
}

func workerFactKey(workerName string, fact expert.Fact) string {
	if strings.TrimSpace(fact.Key) != "" {
		return fact.Key
	}
	return outcomeFactKey("worker:"+workerName, expert.Outcome{
		Name:   fact.Type,
		Params: fact.Fields,
	})
}
