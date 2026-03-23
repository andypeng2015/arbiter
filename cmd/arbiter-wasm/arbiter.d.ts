/** Arbiter WASM SDK — all four evaluation modes in one module. */

/** Initialize the WASM runtime. Must be called before any other function. */
export function init(wasmPath: string): Promise<void>;

// --- Compilation ---

export interface CompileResult {
  rules: number;
  strategies: number;
  error?: string;
}

/** Compile .arb source. Must be called before eval/strategy/session functions. */
export function compile(source: string): CompileResult;

// --- Stateless Evaluation ---

export interface MatchedRule {
  name: string;
  action: string;
  params: Record<string, unknown>;
}

/** Evaluate compiled rules against a JSON context. */
export function eval(jsonContext: string): MatchedRule[] | { error: string };

/** Evaluate with governance (kill switches, rollouts, segments). */
export function evalGoverned(jsonContext: string): MatchedRule[] | { error: string };

// --- Strategies ---

export interface StrategyResult {
  strategy: string;
  outcome: string;
  selected: string;
  params: Record<string, unknown>;
  error?: string;
}

/** Evaluate a named strategy against a JSON context. */
export function evalStrategy(name: string, jsonContext: string): StrategyResult;

// --- Expert Sessions ---

export interface StartSessionResult {
  sessionId: string;
  error?: string;
}

export interface ExpertOutcome {
  rule: string;
  name: string;
  params: Record<string, unknown>;
}

export interface ExpertFact {
  type: string;
  key: string;
  fields: Record<string, unknown>;
}

export interface SessionResult {
  outcomes: ExpertOutcome[];
  facts: ExpertFact[];
  stopReason: string;
  rounds: number;
  mutations: number;
  error?: string;
}

/** Create an expert inference session with an envelope and optional initial facts. */
export function startSession(jsonEnvelope: string, jsonFacts?: string): StartSessionResult;

/** Assert a fact into a session's working memory. */
export function assertFact(sessionId: string, jsonFact: string): { ok: boolean; error?: string };

/** Retract a fact from a session's working memory. */
export function retractFact(sessionId: string, factType: string, factKey: string): { ok: boolean; error?: string };

/** Run an expert session to quiescence. Returns outcomes, facts, and execution metadata. */
export function runSession(sessionId: string): SessionResult;

/** Close and dispose of an expert session. */
export function closeSession(sessionId: string): { ok: boolean };

// --- Workflows ---

export interface WorkflowCompileResult {
  sources: string[];
  error?: string;
}

export interface ArbiterRun {
  outcomes: ExpertOutcome[];
  facts: ExpertFact[];
  stopReason: string;
  rounds: number;
  mutations: number;
}

export interface WorkflowResult {
  order: string[];
  arbiters: Record<string, ArbiterRun>;
  error?: string;
}

/** Compile a workflow from .arb source containing arbiter declarations. */
export function compileWorkflow(source: string): WorkflowCompileResult;

/** Inject facts into an external source target. */
export function setSourceFacts(target: string, jsonFacts: string): { ok: boolean; error?: string };

/** Execute one workflow pass across all arbiters in topological order. */
export function runWorkflow(): WorkflowResult;
