// Package engine implements the concurrent Directed Acyclic Graph (DAG)
// execution loop for Caliper. The DAGEngine accepts a slice of core.Evaluator
// nodes, validates the dependency graph, and executes nodes as their
// dependencies complete — with full concurrency between independent branches.
package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/pranavkakde/caliper/pkg/core"
)

// ---------------------------------------------------------------------------
// nodeStatus — lifecycle states for a single node within one Run call.
// ---------------------------------------------------------------------------

// nodeStatus is the lifecycle state of an evaluator node within one Run.
type nodeStatus uint8

const (
	// statusPending: node is waiting for one or more dependencies to complete.
	statusPending nodeStatus = iota

	// statusReady: all dependencies have passed; goroutine not yet spawned.
	statusReady

	// statusRunning: Evaluate() is executing inside a worker goroutine.
	statusRunning

	// statusDone: Evaluate() has returned; node is in its terminal state.
	// A Done node may have Passed==true or Passed==false.
	statusDone

	// statusSkipped: an upstream dependency did not pass. Evaluate() will
	// never be called on this node; a synthetic failed result is recorded.
	statusSkipped
)

// ---------------------------------------------------------------------------
// DAGEngine — immutable graph topology, concurrency-safe for parallel Runs.
// ---------------------------------------------------------------------------

// DAGEngine holds the validated, immutable topology of an evaluator graph.
// After construction via New, it is safe to call Run from multiple goroutines
// simultaneously (e.g. one goroutine per test case).
type DAGEngine struct {
	nodes    map[string]core.Evaluator // nodeID -> evaluator implementation
	revDeps  map[string][]string       // nodeID -> IDs of nodes that depend on it
	inDegree map[string]int            // nodeID -> number of direct dependencies
	order    []string                  // topological ordering (deps before dependents)
}

// New validates the evaluator slice and constructs a ready-to-use DAGEngine.
//
// Validation performed:
//   - Every evaluator must have a non-empty ID.
//   - No two evaluators may share the same ID.
//   - Every ID listed in an evaluator's Dependencies() must resolve to a
//     declared evaluator in the same slice.
//   - The dependency graph must be acyclic (detected via Kahn's algorithm).
func New(evaluators []core.Evaluator) (*DAGEngine, error) {
	if len(evaluators) == 0 {
		return nil, fmt.Errorf("dag: at least one evaluator is required")
	}

	// Index evaluators by ID.
	nodes := make(map[string]core.Evaluator, len(evaluators))
	for _, ev := range evaluators {
		id := ev.ID()
		if id == "" {
			return nil, fmt.Errorf("dag: an evaluator has an empty ID")
		}
		if _, dup := nodes[id]; dup {
			return nil, fmt.Errorf("dag: duplicate evaluator ID %q", id)
		}
		nodes[id] = ev
	}

	// Build inDegree and reverse-dependency maps.
	inDegree := make(map[string]int, len(nodes))
	revDeps := make(map[string][]string, len(nodes))
	for id := range nodes {
		inDegree[id] = 0 // ensure every node has an entry even if it has no deps
	}
	for _, ev := range evaluators {
		for _, dep := range ev.Dependencies() {
			if _, ok := nodes[dep]; !ok {
				return nil, fmt.Errorf(
					"dag: evaluator %q declares unknown dependency %q",
					ev.ID(), dep,
				)
			}
			revDeps[dep] = append(revDeps[dep], ev.ID())
			inDegree[ev.ID()]++
		}
	}

	order, err := topoSort(nodes, inDegree, revDeps)
	if err != nil {
		return nil, err
	}

	return &DAGEngine{
		nodes:    nodes,
		revDeps:  revDeps,
		inDegree: inDegree,
		order:    order,
	}, nil
}

// topoSort runs Kahn's algorithm over the graph to:
//  1. Produce a deterministic topological ordering of node IDs.
//  2. Detect cycles (if not all nodes can be ordered, there is a cycle).
//
// inDegree and revDeps are read-only; a local copy of inDegree is used to
// avoid mutating the caller's map.
func topoSort(
	nodes map[string]core.Evaluator,
	inDegree map[string]int,
	revDeps map[string][]string,
) ([]string, error) {
	// Work on a copy so we don't mutate the DAGEngine's inDegree.
	deg := make(map[string]int, len(inDegree))
	for k, v := range inDegree {
		deg[k] = v
	}

	// Seed queue with root nodes (no dependencies).
	queue := make([]string, 0, len(nodes))
	for id, d := range deg {
		if d == 0 {
			queue = append(queue, id)
		}
	}

	order := make([]string, 0, len(nodes))
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		order = append(order, cur)
		for _, child := range revDeps[cur] {
			deg[child]--
			if deg[child] == 0 {
				queue = append(queue, child)
			}
		}
	}

	if len(order) != len(nodes) {
		return nil, fmt.Errorf(
			"dag: cycle detected — %d node(s) are part of a circular dependency",
			len(nodes)-len(order),
		)
	}
	return order, nil
}

// ---------------------------------------------------------------------------
// Run — concurrent DAG execution
// ---------------------------------------------------------------------------

// workerMsg is the message a worker goroutine sends back to the coordinator
// once Evaluate() has returned.
type workerMsg struct {
	result core.EvaluationResult
}

// Run executes the full evaluator DAG for a single EvaluationContext (i.e.,
// one test case). It is safe to call concurrently with distinct contexts.
//
// # Concurrency Model
//
// A single coordinator goroutine (the caller of Run) owns all mutable state:
// remaining dep counts, node status, and the results map. This eliminates
// races on that state without needing a mutex.
//
// Worker goroutines are spawned one-per-node and communicate with the
// coordinator exclusively via buffered channels:
//
//   - readyCh (coordinator → workers): carries IDs of nodes whose
//     dependencies have all passed and are ready to execute.
//   - resultCh (workers → coordinator): carries completed EvaluationResults.
//   - errCh (workers → coordinator): carries infrastructure errors (recovered
//     panics). On first receipt the coordinator drains in-flight workers
//     and returns the error.
//
// sync.WaitGroup tracks all live worker goroutines so Run never returns
// while any goroutine is still active.
//
// sync.Mutex (ecMu) serialises concurrent calls to EvaluationContext.Set
// because independent branches of the DAG may write to it simultaneously.
//
// # Skip Semantics
//
// When a node finishes with Passed==false (or a non-nil Err), every
// transitive dependent is immediately marked Skipped by the coordinator —
// without spawning goroutines — and a synthetic failed EvaluationResult is
// recorded for each. This keeps the result slice complete.
//
// # Return Values
//
// The returned slice is ordered topologically (dependencies before
// dependents) and contains exactly one EvaluationResult per node.
//
// A non-nil error is returned only for infrastructure failures (context
// cancellation, recovered panics). Evaluator assertion failures are not
// errors; they surface via EvaluationResult.Passed==false.
func (d *DAGEngine) Run(
	ctx context.Context,
	ec core.EvaluationContext,
) ([]core.EvaluationResult, error) {
	// -----------------------------------------------------------------------
	// Per-run state — owned exclusively by the coordinator goroutine.
	// -----------------------------------------------------------------------
	remaining := make(map[string]int, len(d.nodes)) // unsatisfied dep count per node
	status := make(map[string]nodeStatus, len(d.nodes))
	results := make(map[string]core.EvaluationResult, len(d.nodes))

	for id, deg := range d.inDegree {
		remaining[id] = deg
		status[id] = statusPending
	}

	settled := 0           // nodes that have reached Done or Skipped
	total := len(d.nodes)

	// Buffered to total so the coordinator never blocks on send.
	readyCh := make(chan string, total)
	resultCh := make(chan workerMsg, total)
	errCh := make(chan error, total) // total gives every goroutine a send slot

	// ecMu serialises concurrent EvaluationContext.Set calls from independent
	// worker goroutines that are executing in parallel DAG branches.
	var ecMu sync.Mutex

	// wg tracks all live worker goroutines.
	var wg sync.WaitGroup

	// skipSubtree marks id and all transitive dependents as Skipped (in the
	// coordinator goroutine, no locking needed). Returns the count of nodes
	// newly moved into the Skipped state so settled can be updated correctly.
	var skipSubtree func(id string) int
	skipSubtree = func(id string) int {
		if status[id] == statusSkipped || status[id] == statusDone {
			return 0 // already in a terminal state; don't double-count
		}
		status[id] = statusSkipped
		results[id] = core.EvaluationResult{
			EvaluatorID: id,
			Passed:      false,
			Details: map[string]any{
				"skipped_reason": "upstream dependency did not pass",
			},
		}
		count := 1
		for _, child := range d.revDeps[id] {
			count += skipSubtree(child)
		}
		return count
	}

	// Seed root nodes (in-degree == 0) onto readyCh.
	for id, deg := range d.inDegree {
		if deg == 0 {
			status[id] = statusReady
			readyCh <- id
		}
	}

	// -----------------------------------------------------------------------
	// Coordinator loop.
	// Runs entirely in the calling goroutine. All mutable state is local to
	// this scope; worker goroutines never touch it directly.
	// -----------------------------------------------------------------------
	for settled < total {
		select {

		case <-ctx.Done():
			wg.Wait()
			return nil, fmt.Errorf("dag: run cancelled: %w", ctx.Err())

		case err := <-errCh:
			// A worker goroutine panicked. Stop accepting new work and wait
			// for all in-flight goroutines to exit before returning.
			wg.Wait()
			return nil, err

		case id := <-readyCh:
			// Transition node to Running and spawn a worker goroutine.
			status[id] = statusRunning
			ev := d.nodes[id]

			wg.Add(1)
			go func(nodeID string, evaluator core.Evaluator) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						errCh <- fmt.Errorf(
							"dag: panic in evaluator %q: %v", nodeID, r,
						)
					}
				}()

				start := time.Now()
				res, evalErr := evaluator.Evaluate(ctx, ec)

				// Normalise the result fields.
				res.EvaluatorID = nodeID
				if res.LatencyMS == 0 {
					// Evaluator did not self-report latency; measure wall clock.
					res.LatencyMS = time.Since(start).Milliseconds()
				}
				if evalErr != nil && res.Err == nil {
					res.Err = evalErr
				}
				if res.Err != nil {
					// Any infrastructure error implies a failed assertion.
					res.Passed = false
				}

				// On success, publish this node's result into the shared
				// EvaluationContext so downstream nodes can read it via Get.
				// ecMu serialises concurrent writes from parallel branches.
				if res.Passed {
					ecMu.Lock()
					ec.Set(nodeID+":result", res)
					ecMu.Unlock()
				}

				resultCh <- workerMsg{result: res}
			}(id, ev)

		case msg := <-resultCh:
			// A worker has finished. Update state and notify dependents.
			nodeID := msg.result.EvaluatorID
			results[nodeID] = msg.result
			status[nodeID] = statusDone
			settled++

			if !msg.result.Passed {
				// Skip all transitive dependents immediately so their
				// synthetic results are included in the final slice.
				for _, child := range d.revDeps[nodeID] {
					settled += skipSubtree(child)
				}
			} else {
				// Decrement remaining dep count for each direct dependent.
				// When a dependent's count reaches zero it is enqueued.
				for _, child := range d.revDeps[nodeID] {
					if status[child] == statusSkipped {
						// Already skipped by a different failing dependency;
						// do not enqueue or double-decrement.
						continue
					}
					remaining[child]--
					if remaining[child] == 0 {
						status[child] = statusReady
						readyCh <- child
					}
				}
			}
		}
	}

	// All nodes are in a terminal state. Wait for any goroutine teardown
	// (e.g. deferred wg.Done after a send on resultCh).
	wg.Wait()

	// Assemble results in topological order: dependencies always precede
	// the nodes that depend on them.
	ordered := make([]core.EvaluationResult, 0, len(d.order))
	for _, id := range d.order {
		ordered = append(ordered, results[id])
	}
	return ordered, nil
}

// ---------------------------------------------------------------------------
// Accessors
// ---------------------------------------------------------------------------

// NodeCount returns the number of evaluator nodes in this DAG.
func (d *DAGEngine) NodeCount() int { return len(d.nodes) }

// TopologicalOrder returns a copy of the node IDs in dependency-resolved
// order (dependencies before dependents). Safe to modify.
func (d *DAGEngine) TopologicalOrder() []string {
	cp := make([]string, len(d.order))
	copy(cp, d.order)
	return cp
}

// ---------------------------------------------------------------------------
// Package-level helpers
// ---------------------------------------------------------------------------

// AllPassed reports whether every result in the slice has Passed==true and a
// nil Err. It returns true for an empty slice (vacuous truth). This is the
// canonical way for the evaluate command to determine the CLI exit code.
func AllPassed(results []core.EvaluationResult) bool {
	for _, r := range results {
		if !r.Passed || r.Err != nil {
			return false
		}
	}
	return true
}
