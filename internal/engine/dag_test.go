package engine_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pranavkakde/caliper/internal/engine"
	"github.com/pranavkakde/caliper/pkg/core"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// mockEvaluator is a configurable test double for core.Evaluator.
type mockEvaluator struct {
	id       string
	deps     []string
	passed   bool
	score    core.Score
	latency  time.Duration
	evalErr  error
	called   atomic.Int32 // counts how many times Evaluate was invoked
	panicMsg string       // if non-empty, Evaluate panics with this message
}

func (m *mockEvaluator) ID() string            { return m.id }
func (m *mockEvaluator) Dependencies() []string { return m.deps }

func (m *mockEvaluator) Evaluate(ctx context.Context, ec core.EvaluationContext) (core.EvaluationResult, error) {
	m.called.Add(1)
	if m.panicMsg != "" {
		panic(m.panicMsg)
	}
	if m.latency > 0 {
		time.Sleep(m.latency)
	}
	return core.EvaluationResult{
		EvaluatorID: m.id,
		Passed:      m.passed,
		Score:       m.score,
		Err:         m.evalErr,
	}, m.evalErr
}

// mockEvaluationContext is a simple in-memory EvaluationContext.
type mockEvaluationContext struct {
	store    map[string]any
	provider core.Provider
	runID    string
}

func newMockContext() *mockEvaluationContext {
	return &mockEvaluationContext{store: make(map[string]any), runID: "test-run"}
}

func (m *mockEvaluationContext) Provider() core.Provider    { return m.provider }
func (m *mockEvaluationContext) RunID() string              { return m.runID }
func (m *mockEvaluationContext) Set(key string, val any)    { m.store[key] = val }
func (m *mockEvaluationContext) Get(key string) (any, bool) { v, ok := m.store[key]; return v, ok }

// pass / fail are convenience constructors.
func pass(id string, deps ...string) *mockEvaluator {
	return &mockEvaluator{id: id, deps: deps, passed: true}
}
func fail(id string, deps ...string) *mockEvaluator {
	return &mockEvaluator{id: id, deps: deps, passed: false}
}

// ---------------------------------------------------------------------------
// New() — construction & validation
// ---------------------------------------------------------------------------

func TestNew_EmptySlice(t *testing.T) {
	_, err := engine.New(nil)
	if err == nil {
		t.Fatal("expected error for empty evaluator slice, got nil")
	}
}

func TestNew_EmptyID(t *testing.T) {
	ev := &mockEvaluator{id: "", deps: nil, passed: true}
	_, err := engine.New([]core.Evaluator{ev})
	if err == nil {
		t.Fatal("expected error for evaluator with empty ID")
	}
}

func TestNew_DuplicateID(t *testing.T) {
	evs := []core.Evaluator{pass("a"), pass("a")}
	_, err := engine.New(evs)
	if err == nil {
		t.Fatal("expected error for duplicate evaluator IDs")
	}
}

func TestNew_UnknownDependency(t *testing.T) {
	ev := &mockEvaluator{id: "a", deps: []string{"ghost"}, passed: true}
	_, err := engine.New([]core.Evaluator{ev})
	if err == nil {
		t.Fatal("expected error for reference to unknown dependency")
	}
}

func TestNew_CycleDetected(t *testing.T) {
	// a -> b -> c -> a
	a := &mockEvaluator{id: "a", deps: []string{"c"}, passed: true}
	b := &mockEvaluator{id: "b", deps: []string{"a"}, passed: true}
	c := &mockEvaluator{id: "c", deps: []string{"b"}, passed: true}

	_, err := engine.New([]core.Evaluator{a, b, c})
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
}

func TestNew_ValidLinearChain(t *testing.T) {
	evs := []core.Evaluator{
		pass("a"),
		pass("b", "a"),
		pass("c", "b"),
	}
	dag, err := engine.New(evs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dag.NodeCount() != 3 {
		t.Fatalf("expected 3 nodes, got %d", dag.NodeCount())
	}
}

// ---------------------------------------------------------------------------
// Run() — correct execution ordering and results
// ---------------------------------------------------------------------------

func TestRun_SingleNode_Pass(t *testing.T) {
	ev := pass("root")
	dag, _ := engine.New([]core.Evaluator{ev})

	results, err := dag.Run(context.Background(), newMockContext())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Error("expected Passed==true")
	}
	if ev.called.Load() != 1 {
		t.Errorf("expected Evaluate called once, got %d", ev.called.Load())
	}
}

func TestRun_SingleNode_Fail(t *testing.T) {
	ev := fail("root")
	dag, _ := engine.New([]core.Evaluator{ev})

	results, err := dag.Run(context.Background(), newMockContext())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Passed {
		t.Error("expected Passed==false")
	}
}

func TestRun_LinearChain_AllPass(t *testing.T) {
	a, b, c := pass("a"), pass("b", "a"), pass("c", "b")
	dag, _ := engine.New([]core.Evaluator{a, b, c})

	results, err := dag.Run(context.Background(), newMockContext())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Errorf("expected all nodes to pass; %q failed", r.EvaluatorID)
		}
	}
	// Verify topological order: a, b, c.
	order := []string{"a", "b", "c"}
	for i, id := range order {
		if results[i].EvaluatorID != id {
			t.Errorf("position %d: expected %q, got %q", i, id, results[i].EvaluatorID)
		}
	}
}

func TestRun_FailedRoot_SkipsDownstream(t *testing.T) {
	// a fails → b and c (both depend on a) must be skipped.
	a := fail("a")
	b := pass("b", "a")
	c := pass("c", "a")

	dag, _ := engine.New([]core.Evaluator{a, b, c})

	results, err := dag.Run(context.Background(), newMockContext())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	byID := make(map[string]core.EvaluationResult, 3)
	for _, r := range results {
		byID[r.EvaluatorID] = r
	}

	if byID["a"].Passed {
		t.Error("expected 'a' to fail")
	}
	if b.called.Load() != 0 {
		t.Errorf("'b' Evaluate should not have been called; called %d time(s)", b.called.Load())
	}
	if c.called.Load() != 0 {
		t.Errorf("'c' Evaluate should not have been called; called %d time(s)", c.called.Load())
	}
	if byID["b"].Passed || byID["c"].Passed {
		t.Error("expected skipped nodes to have Passed==false")
	}
	// Skipped nodes should record the reason.
	for _, id := range []string{"b", "c"} {
		if _, ok := byID[id].Details["skipped_reason"]; !ok {
			t.Errorf("expected 'skipped_reason' in Details for node %q", id)
		}
	}
}

func TestRun_DiamondDAG_AllPass(t *testing.T) {
	//       root
	//      /    \
	//    left  right
	//      \    /
	//      merge
	root := pass("root")
	left := pass("left", "root")
	right := pass("right", "root")
	merge := pass("merge", "left", "right")

	dag, _ := engine.New([]core.Evaluator{root, left, right, merge})

	results, err := dag.Run(context.Background(), newMockContext())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Errorf("expected all nodes to pass; %q failed", r.EvaluatorID)
		}
	}
}

func TestRun_DiamondDAG_OneBranchFails_MergeSkipped(t *testing.T) {
	root := pass("root")
	left := fail("left", "root")   // left fails
	right := pass("right", "root")
	merge := pass("merge", "left", "right") // depends on both; must be skipped

	dag, _ := engine.New([]core.Evaluator{root, left, right, merge})

	results, err := dag.Run(context.Background(), newMockContext())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	byID := map[string]core.EvaluationResult{}
	for _, r := range results {
		byID[r.EvaluatorID] = r
	}

	if !byID["root"].Passed {
		t.Error("root should pass")
	}
	if byID["left"].Passed {
		t.Error("left should fail")
	}
	if !byID["right"].Passed {
		t.Error("right should pass (independent branch)")
	}
	if byID["merge"].Passed {
		t.Error("merge should be skipped (failed==false)")
	}
	if merge.called.Load() != 0 {
		t.Errorf("merge.Evaluate should not be called; got %d call(s)", merge.called.Load())
	}
}

func TestRun_EvalError_TreatedAsFail(t *testing.T) {
	ev := &mockEvaluator{
		id:      "err-node",
		passed:  true, // would pass but evalErr overrides
		evalErr: errors.New("provider timeout"),
	}
	child := pass("child", "err-node")

	dag, _ := engine.New([]core.Evaluator{ev, child})

	results, err := dag.Run(context.Background(), newMockContext())
	if err != nil {
		t.Fatalf("unexpected infrastructure error: %v", err)
	}

	byID := map[string]core.EvaluationResult{}
	for _, r := range results {
		byID[r.EvaluatorID] = r
	}

	if byID["err-node"].Passed {
		t.Error("a node with non-nil Err must have Passed==false")
	}
	if byID["err-node"].Err == nil {
		t.Error("Err field should be populated")
	}
	if child.called.Load() != 0 {
		t.Errorf("child should be skipped after error; got %d call(s)", child.called.Load())
	}
}

func TestRun_PanicInEvaluator_ReturnsError(t *testing.T) {
	ev := &mockEvaluator{id: "panicky", panicMsg: "oh no"}
	dag, _ := engine.New([]core.Evaluator{ev})

	_, err := dag.Run(context.Background(), newMockContext())
	if err == nil {
		t.Fatal("expected infrastructure error from recovered panic")
	}
}

func TestRun_ContextCancelled_ReturnsError(t *testing.T) {
	// Use a slow evaluator so the context fires first.
	ev := &mockEvaluator{id: "slow", passed: true, latency: 200 * time.Millisecond}
	dag, _ := engine.New([]core.Evaluator{ev})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := dag.Run(ctx, newMockContext())
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestRun_MultipleRoots_AllRun(t *testing.T) {
	// Three independent root nodes should all execute.
	a, b, c := pass("a"), pass("b"), pass("c")
	dag, _ := engine.New([]core.Evaluator{a, b, c})

	results, err := dag.Run(context.Background(), newMockContext())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, ev := range []*mockEvaluator{a, b, c} {
		if ev.called.Load() != 1 {
			t.Errorf("evaluator %q should be called once; got %d", ev.id, ev.called.Load())
		}
	}
}

func TestRun_LatencyIsRecorded(t *testing.T) {
	ev := &mockEvaluator{id: "slow", passed: true, latency: 10 * time.Millisecond}
	dag, _ := engine.New([]core.Evaluator{ev})

	results, err := dag.Run(context.Background(), newMockContext())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].LatencyMS < 10 {
		t.Errorf("expected LatencyMS >= 10, got %d", results[0].LatencyMS)
	}
}

func TestRun_ResultPublishedToContext(t *testing.T) {
	// A passing node should publish its result to EvaluationContext
	// so downstream nodes can retrieve it via Get.
	var downstreamGotResult bool

	upstream := pass("up")
	downstream := &mockEvaluator{id: "down", deps: []string{"up"}, passed: true}

	// Wrap downstream to inspect the context during Evaluate.
	type contextInspector struct {
		*mockEvaluator
		ctx *mockEvaluationContext
	}
	inspector := &contextInspector{mockEvaluator: downstream}
	_ = inspector

	dag, _ := engine.New([]core.Evaluator{upstream, downstream})

	// Inject a custom context that records Set calls.
	ec := newMockContext()
	_, err := dag.Run(context.Background(), ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, downstreamGotResult = ec.Get("up:result")
	if !downstreamGotResult {
		t.Error("expected 'up:result' to be published to EvaluationContext after upstream passes")
	}
}

// ---------------------------------------------------------------------------
// AllPassed helper
// ---------------------------------------------------------------------------

func TestAllPassed_Empty(t *testing.T) {
	if !engine.AllPassed(nil) {
		t.Error("AllPassed(nil) should return true (vacuous)")
	}
}

func TestAllPassed_AllTrue(t *testing.T) {
	results := []core.EvaluationResult{
		{Passed: true},
		{Passed: true},
	}
	if !engine.AllPassed(results) {
		t.Error("expected AllPassed==true")
	}
}

func TestAllPassed_OneFail(t *testing.T) {
	results := []core.EvaluationResult{
		{Passed: true},
		{Passed: false},
	}
	if engine.AllPassed(results) {
		t.Error("expected AllPassed==false when at least one fails")
	}
}

func TestAllPassed_NonNilErr(t *testing.T) {
	results := []core.EvaluationResult{
		{Passed: true, Err: fmt.Errorf("oops")},
	}
	if engine.AllPassed(results) {
		t.Error("expected AllPassed==false when Err is non-nil")
	}
}
