// Package evaluation provides the concrete, thread-safe implementation of
// core.EvaluationContext used during a live DAG run.
package evaluation

import (
	"sync"

	"github.com/pranavkakde/caliper/pkg/core"
)

// RunContext is the production implementation of core.EvaluationContext.
// It is safe for concurrent use: Get uses a read-lock so independent DAG
// branches can read simultaneously, while Set uses a write-lock.
//
// The evaluate command creates one RunContext per test case, pre-populates
// the "prompt", "response", and (optionally) "expected" keys, then passes
// it to engine.DAGEngine.Run.
type RunContext struct {
	mu       sync.RWMutex
	store    map[string]any
	provider core.Provider
	runID    string
}

// NewRunContext creates a RunContext bound to the given provider and run ID.
// The store is initialised empty; callers should populate well-known keys
// (e.g. "prompt", "response") before passing to the DAG engine.
func NewRunContext(runID string, provider core.Provider) *RunContext {
	return &RunContext{
		store:    make(map[string]any),
		provider: provider,
		runID:    runID,
	}
}

// Provider returns the LLM backend bound to this run.
func (c *RunContext) Provider() core.Provider { return c.provider }

// RunID returns the unique identifier for this evaluation run.
func (c *RunContext) RunID() string { return c.runID }

// Set stores val under key. Safe for concurrent use.
func (c *RunContext) Set(key string, val any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = val
}

// Get retrieves the value stored under key. Safe for concurrent use.
func (c *RunContext) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.store[key]
	return v, ok
}
