package evaluator_test

import (
	"context"
	"testing"
	"time"

	"github.com/pranavkakde/caliper/internal/config"
	"github.com/pranavkakde/caliper/internal/evaluator"
	"github.com/pranavkakde/caliper/internal/storage"
	"github.com/pranavkakde/caliper/pkg/core"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// contextWithResult pre-seeds an evaluation context with a dependency result
// exactly as the DAG engine would.
type mapContext struct {
	store map[string]any
}

func newCtx(pairs ...any) *mapContext {
	m := &mapContext{store: make(map[string]any)}
	for i := 0; i+1 < len(pairs); i += 2 {
		m.store[pairs[i].(string)] = pairs[i+1]
	}
	return m
}

func (m *mapContext) Provider() core.Provider    { return nil }
func (m *mapContext) RunID() string              { return "test-run" }
func (m *mapContext) Set(k string, v any)        { m.store[k] = v }
func (m *mapContext) Get(k string) (any, bool)   { v, ok := m.store[k]; return v, ok }

func passResult(id string, latency int64, score float64) core.EvaluationResult {
	return core.EvaluationResult{
		EvaluatorID: id,
		Passed:      true,
		LatencyMS:   latency,
		Score:       core.Score{Normalized: score, Weight: 1.0},
	}
}

func makeBaseline(avgLatency int64, overallScore float64) *storage.RunRecord {
	return &storage.RunRecord{
		RunID:     "baseline-1",
		Timestamp: time.Now().Add(-24 * time.Hour),
		DatasetID: "test-ds",
		Telemetry: storage.Telemetry{
			AvgLatencyMS: avgLatency,
			OverallScore: overallScore,
			CostUSD:      0.0,
			Passed:       true,
		},
	}
}

// ---------------------------------------------------------------------------
// Build factory — regression type
// ---------------------------------------------------------------------------

func TestBuild_RegressionType(t *testing.T) {
	cfgs := []config.EvaluatorConfig{
		{ID: "reg", Type: "regression", DependsOn: []string{}},
	}
	opts := evaluator.BuildOptions{Baseline: nil}
	evs, err := evaluator.Build(cfgs, opts)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("expected 1 evaluator, got %d", len(evs))
	}
	if evs[0].ID() != "reg" {
		t.Errorf("expected ID=%q, got %q", "reg", evs[0].ID())
	}
}

func TestBuild_UnknownType(t *testing.T) {
	cfgs := []config.EvaluatorConfig{
		{ID: "x", Type: "llm-judge"},
	}
	_, err := evaluator.Build(cfgs, evaluator.BuildOptions{})
	if err == nil {
		t.Fatal("expected error for unknown evaluator type")
	}
}

// ---------------------------------------------------------------------------
// RegressionEvaluator.Evaluate — no baseline (first run)
// ---------------------------------------------------------------------------

func TestRegression_NoBaseline_PassesVacuously(t *testing.T) {
	cfgs := []config.EvaluatorConfig{
		{ID: "reg", Type: "regression", DependsOn: []string{}},
	}
	evs, _ := evaluator.Build(cfgs, evaluator.BuildOptions{Baseline: nil})
	res, err := evs[0].Evaluate(context.Background(), newCtx())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Passed {
		t.Error("expected Passed==true on first run (no baseline)")
	}
	if _, ok := res.Details["notice"]; !ok {
		t.Error("expected 'notice' in Details for first run")
	}
}

// ---------------------------------------------------------------------------
// RegressionEvaluator.Evaluate — with baseline, all thresholds respected
// ---------------------------------------------------------------------------

func TestRegression_AllWithinThresholds_Passes(t *testing.T) {
	baseline := makeBaseline(100, 1.0) // 100ms avg, score 1.0

	thresholds := config.RegressionConfig{
		MaxLatencyRegressionPct: 50, // allow up to 50% slower
		MaxScoreRegressionPct:   10, // allow up to 10% score drop
	}
	cfgs := []config.EvaluatorConfig{
		{
			ID:        "reg",
			Type:      "regression",
			DependsOn: []string{"a", "b"},
			Params: map[string]any{
				"max_latency_regression_pct": 50.0,
				"max_score_regression_pct":   10.0,
			},
		},
	}
	opts := evaluator.BuildOptions{Baseline: baseline, RegressionCfg: thresholds}
	evs, _ := evaluator.Build(cfgs, opts)

	// Current run: 120ms avg (20% slower — within 50% limit), score 0.95 (5% drop — within 10%)
	ec := newCtx(
		"a:result", passResult("a", 120, 0.95),
		"b:result", passResult("b", 120, 0.95),
		"cost_usd", 0.0,
	)
	res, err := evs[0].Evaluate(context.Background(), ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected Passed==true; details: %v", res.Details)
	}
}

// ---------------------------------------------------------------------------
// RegressionEvaluator.Evaluate — latency regression
// ---------------------------------------------------------------------------

func TestRegression_LatencyBreach_Fails(t *testing.T) {
	baseline := makeBaseline(100, 1.0)

	cfgs := []config.EvaluatorConfig{
		{
			ID:        "reg",
			Type:      "regression",
			DependsOn: []string{"a"},
			Params: map[string]any{
				"max_latency_regression_pct": 20.0, // allow up to 20% slower
			},
		},
	}
	opts := evaluator.BuildOptions{Baseline: baseline}
	evs, _ := evaluator.Build(cfgs, opts)

	// Current run: 200ms avg = 100% increase, well above 20% limit.
	ec := newCtx(
		"a:result", passResult("a", 200, 1.0),
		"cost_usd", 0.0,
	)
	res, err := evs[0].Evaluate(context.Background(), ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Error("expected Passed==false on latency regression")
	}
	if _, ok := res.Details["breaches"]; !ok {
		t.Error("expected 'breaches' in Details when thresholds are exceeded")
	}
}

// ---------------------------------------------------------------------------
// RegressionEvaluator.Evaluate — score regression
// ---------------------------------------------------------------------------

func TestRegression_ScoreBreach_Fails(t *testing.T) {
	baseline := makeBaseline(10, 1.0)

	cfgs := []config.EvaluatorConfig{
		{
			ID:        "reg",
			Type:      "regression",
			DependsOn: []string{"a"},
			Params: map[string]any{
				"max_score_regression_pct": 5.0, // allow up to 5% score drop
			},
		},
	}
	opts := evaluator.BuildOptions{Baseline: baseline}
	evs, _ := evaluator.Build(cfgs, opts)

	// Score drops from 1.0 to 0.80 = 20% drop, above the 5% limit.
	ec := newCtx(
		"a:result", passResult("a", 10, 0.80),
		"cost_usd", 0.0,
	)
	res, _ := evs[0].Evaluate(context.Background(), ec)
	if res.Passed {
		t.Error("expected Passed==false on score regression")
	}
}

// ---------------------------------------------------------------------------
// RegressionEvaluator.Evaluate — cost regression
// ---------------------------------------------------------------------------

func TestRegression_CostBreach_Fails(t *testing.T) {
	baseline := makeBaseline(10, 1.0)
	baseline.Telemetry.CostUSD = 0.01

	cfgs := []config.EvaluatorConfig{
		{
			ID:        "reg",
			Type:      "regression",
			DependsOn: []string{"a"},
			Params: map[string]any{
				"max_cost_regression_usd": 0.005, // allow up to $0.005 more
			},
		},
	}
	opts := evaluator.BuildOptions{Baseline: baseline}
	evs, _ := evaluator.Build(cfgs, opts)

	// Cost increases by $0.02, above the $0.005 limit.
	ec := newCtx(
		"a:result", passResult("a", 10, 1.0),
		"cost_usd", 0.03,
	)
	res, _ := evs[0].Evaluate(context.Background(), ec)
	if res.Passed {
		t.Error("expected Passed==false on cost regression")
	}
}

// ---------------------------------------------------------------------------
// RegressionEvaluator — zero-latency baseline does not false-positive
// ---------------------------------------------------------------------------

func TestRegression_ZeroBaselineLatency_SkipsLatencyCheck(t *testing.T) {
	baseline := makeBaseline(0, 1.0) // 0ms baseline → latency check skipped

	cfgs := []config.EvaluatorConfig{
		{
			ID:        "reg",
			Type:      "regression",
			DependsOn: []string{"a"},
			Params: map[string]any{
				"max_latency_regression_pct": 20.0,
			},
		},
	}
	opts := evaluator.BuildOptions{Baseline: baseline}
	evs, _ := evaluator.Build(cfgs, opts)

	ec := newCtx(
		"a:result", passResult("a", 9999, 1.0), // huge latency, but baseline is 0 → no check
		"cost_usd", 0.0,
	)
	res, err := evs[0].Evaluate(context.Background(), ec)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Error("expected Passed==true when baseline latency is 0 (skip latency check)")
	}
}
