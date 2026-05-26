package arbiter

import (
	"fmt"

	"m31labs.dev/arbiter/ir"
)

// WorkerDeclaration is one named typed capability that an arbiter can invoke.
type WorkerDeclaration struct {
	Name       string
	Input      string
	Output     string
	OutputKind WorkerOutputKind
	Kind       ArbiterHandlerKind
	Target     string
}

// WorkerOutputKind identifies whether a worker returns facts or outcomes.
type WorkerOutputKind string

const (
	WorkerOutputFact    WorkerOutputKind = "fact"
	WorkerOutputOutcome WorkerOutputKind = "outcome"
)

func compileWorkers(program *ir.Program) (map[string]WorkerDeclaration, error) {
	if program == nil {
		return nil, nil
	}
	out := make(map[string]WorkerDeclaration, len(program.Workers))
	for i := range program.Workers {
		decl, err := compileWorkerDeclaration(program, &program.Workers[i])
		if err != nil {
			return nil, err
		}
		if _, ok := out[decl.Name]; ok {
			return nil, fmt.Errorf("duplicate worker %q", decl.Name)
		}
		out[decl.Name] = decl
	}
	return out, nil
}

func compileWorkerDeclaration(program *ir.Program, worker *ir.Worker) (WorkerDeclaration, error) {
	if worker == nil {
		return WorkerDeclaration{}, fmt.Errorf("nil worker declaration")
	}
	if worker.Name == "" {
		return WorkerDeclaration{}, fmt.Errorf("worker declaration missing name")
	}
	if worker.Input == "" {
		return WorkerDeclaration{}, fmt.Errorf("worker %s: input is required", worker.Name)
	}
	if _, ok := program.OutcomeSchemaByName(worker.Input); !ok {
		return WorkerDeclaration{}, fmt.Errorf("worker %s: input %s must reference an outcome schema", worker.Name, worker.Input)
	}
	if worker.Output == "" {
		return WorkerDeclaration{}, fmt.Errorf("worker %s: output is required", worker.Name)
	}
	outputKind := WorkerOutputOutcome
	if _, ok := program.OutcomeSchemaByName(worker.Output); !ok {
		if _, ok := program.FactSchemaByName(worker.Output); !ok {
			return WorkerDeclaration{}, fmt.Errorf("worker %s: output %s must reference a fact or outcome schema", worker.Name, worker.Output)
		}
		outputKind = WorkerOutputFact
	}

	kind := ArbiterHandlerKind(worker.Kind)
	if !workerRuntimeKindAllowed(kind) {
		return WorkerDeclaration{}, fmt.Errorf("worker %s: unsupported runtime kind %s", worker.Name, worker.Kind)
	}
	if kind != ArbiterHandlerStdout && worker.Target == "" {
		return WorkerDeclaration{}, fmt.Errorf("worker %s: runtime %s requires a target", worker.Name, kind)
	}
	if kind == ArbiterHandlerStdout && worker.Target != "" {
		return WorkerDeclaration{}, fmt.Errorf("worker %s: runtime %s does not take a target", worker.Name, kind)
	}

	return WorkerDeclaration{
		Name:       worker.Name,
		Input:      worker.Input,
		Output:     worker.Output,
		OutputKind: outputKind,
		Kind:       kind,
		Target:     worker.Target,
	}, nil
}

func workerRuntimeKindAllowed(kind ArbiterHandlerKind) bool {
	if kind == "" {
		return false
	}
	switch kind {
	case ArbiterHandlerChain, ArbiterHandlerWorker:
		return false
	default:
		return true
	}
}
