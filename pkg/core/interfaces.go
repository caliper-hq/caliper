// Package core defines the fundamental contracts of the Caliper evaluation
// framework. All concrete implementations (providers, evaluators, reporters)
// must satisfy these interfaces; this package deliberately contains no
// implementation code.
package core

import "context"

// ---------------------------------------------------------------------------
// Score — a richer representation of an evaluator's quality measurement.
// ---------------------------------------------------------------------------

// Score captures multiple dimensions of an evaluator's measurement so that
// the regression engine (Phase 2) can compare runs at the right granularity.
type Score struct {
	// Raw is the unmodified numeric output of the evaluator
	// (e.g. BLEU score, character-overlap ratio, exact-match 0/1).
	Raw float64 `json:"raw"`

	// Normalized is Raw mapped into [0.0, 1.0] so scores from different
	// evaluators can be compared on a common scale.
	Normalized float64 `json:"normalized"`

	// Weight is the relative importance of this evaluator within its
	// DatasetGroup. Used by the reporter to compute a weighted overall score.
	// Defaults to 1.0 if not explicitly set.
	Weight float64 `json:"weight"`
}

// ---------------------------------------------------------------------------
// EvaluationResult — concrete data returned by every Evaluator node.
// ---------------------------------------------------------------------------

// EvaluationResult is a value type (struct, not interface) because it is
// plain data transferred between the DAG engine, reporters, and the storage
// layer. It must remain serialisable to JSON without any behaviour.
type EvaluationResult struct {
	// EvaluatorID matches the ID() of the Evaluator that produced this result.
	EvaluatorID string `json:"evaluator_id"`

	// Passed indicates whether the evaluator's acceptance condition was met.
	Passed bool `json:"passed"`

	// Score holds the quality measurement for this evaluation node.
	Score Score `json:"score"`

	// LatencyMS is the wall-clock time spent inside Evaluate(), in milliseconds.
	LatencyMS int64 `json:"latency_ms"`

	// Details is an open-ended map for evaluator-specific diagnostics
	// (e.g. matched regex, token counts, diff snippets).
	Details map[string]any `json:"details,omitempty"`

	// Err captures any non-fatal error surfaced during evaluation.
	// A non-nil Err implies Passed == false, but Passed == false does not
	// necessarily imply a non-nil Err (a clean failing assertion is valid).
	// Note: the error interface is not JSON-serialisable; the storage layer
	// (Phase 2) will convert this to a string on write.
	Err error `json:"-"`
}

// ---------------------------------------------------------------------------
// Provider — abstracts any LLM backend.
// ---------------------------------------------------------------------------

// Provider is a stateless adapter to an inference backend. Implementations
// must be safe for concurrent use; the DAG engine may call Complete from
// multiple goroutines simultaneously.
type Provider interface {
	// Complete submits a prompt to the backend and returns the raw completion.
	// The caller is responsible for passing a context with an appropriate
	// deadline or cancellation signal.
	Complete(ctx context.Context, prompt string) (string, error)

	// Name returns a human-readable identifier for the backend
	// (e.g. "openai-gpt-4o", "mock"). Used in reports and history files.
	Name() string
}

// ---------------------------------------------------------------------------
// EvaluationContext — the mutable runtime bag threaded through the DAG.
// ---------------------------------------------------------------------------

// EvaluationContext is the shared state object passed to every Evaluator node
// during a single evaluation run. It carries the bound Provider, a typed
// key-value store for inter-node communication, and any run-level metadata.
//
// Implementations must be safe for concurrent reads; writes via Set should
// only occur from the owning evaluator node (the engine serialises node
// execution relative to dependencies, so concurrent writes to the same key
// should not occur in a correctly constructed DAG).
type EvaluationContext interface {
	// Provider returns the LLM backend bound to this evaluation run.
	Provider() Provider

	// Set stores an arbitrary value under key, allowing an upstream evaluator
	// to pass its output to downstream nodes without coupling them directly.
	Set(key string, val any)

	// Get retrieves a value previously stored by Set. The second return value
	// is false if no value is registered under key.
	Get(key string) (any, bool)

	// RunID returns the unique identifier for the current evaluation run,
	// used by the storage layer when persisting results.
	RunID() string
}

// ---------------------------------------------------------------------------
// Evaluator — a single node in the evaluation DAG.
// ---------------------------------------------------------------------------

// Evaluator represents one discrete evaluation step. The DAG engine uses the
// ID and Dependencies methods to construct the execution graph at startup;
// Evaluate is then called once all dependency nodes have completed
// successfully.
//
// Implementations must be safe for concurrent use across different
// EvaluationContext instances.
type Evaluator interface {
	// ID returns the unique, stable identifier for this evaluator node within
	// the DAG. IDs are referenced by other nodes' Dependencies() slices and
	// must be non-empty.
	ID() string

	// Dependencies returns the IDs of all Evaluator nodes that must complete
	// successfully before this node may be executed. Return an empty slice
	// (not nil) for root nodes that have no prerequisites.
	Dependencies() []string

	// Evaluate performs the evaluation and returns a result. If the
	// acceptance condition is not met the result should have Passed == false;
	// Evaluate should only return a non-nil error for unexpected, unrecoverable
	// failures (I/O errors, panics, etc.). A deliberate failing assertion is
	// represented by Passed == false with Err == nil.
	Evaluate(ctx context.Context, ec EvaluationContext) (EvaluationResult, error)
}

// ---------------------------------------------------------------------------
// Reporter — emits the final results of a completed DAG execution.
// ---------------------------------------------------------------------------

// Reporter formats and delivers evaluation results to an output target.
// Multiple reporters may be registered (e.g. console + JSON file + remote
// API). The DAG engine calls Report once after all nodes have settled
// (either completed or failed).
type Reporter interface {
	// Report receives the complete, ordered slice of EvaluationResults and
	// writes them to the reporter's output target. Returning an error does
	// not affect the CLI exit code (results are already determined by the
	// DAG), but the error will be surfaced to the user.
	Report(ctx context.Context, results []EvaluationResult) error
}
