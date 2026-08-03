package evaluator

import (
	"context"
	"fmt"
	"math"

	"github.com/pranavkakde/caliper/internal/config"
	"github.com/pranavkakde/caliper/internal/storage"
	"github.com/pranavkakde/caliper/pkg/core"
)

// RegressionEvaluator is a DAG node that compares the current run's aggregate
// telemetry against a stored baseline and fails if any configured threshold
// is exceeded.
//
// It must be declared last in the evaluators list (depending on all quality
// nodes) so that the DAG engine publishes every sibling result to context
// before this node executes.
//
// Context keys consumed:
//   - "{depID}:result"  — core.EvaluationResult published by the engine for
//     each dependency that passed.
//   - "cost_usd"        — float64 set by evaluate.go (0.0 for mock provider).
//
// Baseline rules:
//   - If baseline is nil (first run), the node passes vacuously and records
//     a notice in Details.
//   - If baseline.Telemetry.AvgLatencyMS == 0 (no latency history), the
//     latency threshold is skipped to avoid divide-by-zero false positives.
type RegressionEvaluator struct {
	id         string
	deps       []string
	baseline   *storage.RunRecord      // nil on first run
	thresholds config.RegressionConfig
}

// newRegression constructs a RegressionEvaluator. baseline may be nil.
func newRegression(
	id string,
	deps []string,
	baseline *storage.RunRecord,
	thresholds config.RegressionConfig,
) *RegressionEvaluator {
	return &RegressionEvaluator{
		id:         id,
		deps:       deps,
		baseline:   baseline,
		thresholds: thresholds,
	}
}

// ID implements core.Evaluator.
func (r *RegressionEvaluator) ID() string { return r.id }

// Dependencies implements core.Evaluator.
func (r *RegressionEvaluator) Dependencies() []string { return r.deps }

// Evaluate implements core.Evaluator.
func (r *RegressionEvaluator) Evaluate(_ context.Context, ec core.EvaluationContext) (core.EvaluationResult, error) {
	// ── No baseline: first run, pass vacuously ──────────────────────
	if r.baseline == nil {
		return core.EvaluationResult{
			EvaluatorID: r.id,
			Passed:      true,
			Score:       core.Score{Raw: 1, Normalized: 1, Weight: 1},
			Details: map[string]any{
				"notice": "no baseline found; this run will become the first baseline",
			},
		}, nil
	}

	// ── Collect current telemetry from dependency results ───────────
	cur := r.computeCurrent(ec)

	// ── Read current cost from context ──────────────────────────────
	if v, ok := ec.Get("cost_usd"); ok {
		if f, ok := v.(float64); ok {
			cur.CostUSD = f
		}
	}

	base := r.baseline.Telemetry

	// ── Compute and check deltas ─────────────────────────────────────
	type breach struct {
		metric   string
		delta    float64
		limit    float64
		unit     string
	}
	var breaches []breach

	// Latency regression (percentage increase).
	if r.thresholds.MaxLatencyRegressionPct > 0 && base.AvgLatencyMS > 0 {
		deltaPct := float64(cur.AvgLatencyMS-base.AvgLatencyMS) /
			float64(base.AvgLatencyMS) * 100
		if deltaPct > r.thresholds.MaxLatencyRegressionPct {
			breaches = append(breaches, breach{
				metric: "avg_latency_ms",
				delta:  round2(deltaPct),
				limit:  r.thresholds.MaxLatencyRegressionPct,
				unit:   "%",
			})
		}
	}

	// Score regression (percentage decrease).
	if r.thresholds.MaxScoreRegressionPct > 0 && base.OverallScore > 0 {
		dropPct := (base.OverallScore - cur.OverallScore) /
			base.OverallScore * 100
		if dropPct > r.thresholds.MaxScoreRegressionPct {
			breaches = append(breaches, breach{
				metric: "overall_score",
				delta:  round2(dropPct),
				limit:  r.thresholds.MaxScoreRegressionPct,
				unit:   "% drop",
			})
		}
	}

	// Cost regression (absolute increase in USD).
	if r.thresholds.MaxCostRegressionUSD > 0 {
		deltaUSD := cur.CostUSD - base.CostUSD
		if deltaUSD > r.thresholds.MaxCostRegressionUSD {
			breaches = append(breaches, breach{
				metric: "cost_usd",
				delta:  round2(deltaUSD),
				limit:  r.thresholds.MaxCostRegressionUSD,
				unit:   "USD",
			})
		}
	}

	passed := len(breaches) == 0

	details := map[string]any{
		"baseline_run_id":        r.baseline.RunID,
		"baseline_avg_latency":   base.AvgLatencyMS,
		"baseline_overall_score": base.OverallScore,
		"baseline_cost_usd":      base.CostUSD,
		"current_avg_latency":    cur.AvgLatencyMS,
		"current_overall_score":  cur.OverallScore,
		"current_cost_usd":       cur.CostUSD,
	}

	if !passed {
		msgs := make([]string, len(breaches))
		for i, b := range breaches {
			msgs[i] = fmt.Sprintf("%s regressed by %.2f%s (limit %.2f%s)",
				b.metric, b.delta, b.unit, b.limit, b.unit)
		}
		details["breaches"] = msgs
	}

	score := 0.0
	if passed {
		score = 1.0
	}

	return core.EvaluationResult{
		EvaluatorID: r.id,
		Passed:      passed,
		Score:       core.Score{Raw: score, Normalized: score, Weight: 1},
		Details:     details,
	}, nil
}

// computeCurrent derives aggregate telemetry from the dependency nodes'
// results, which the DAG engine has published to context as "{depID}:result".
func (r *RegressionEvaluator) computeCurrent(ec core.EvaluationContext) storage.Telemetry {
	var (
		totalLatency int64
		weightedSum  float64
		totalWeight  float64
		count        int
	)

	for _, depID := range r.deps {
		raw, ok := ec.Get(depID + ":result")
		if !ok {
			// Dependency was skipped or failed — its result was not published.
			continue
		}
		res, ok := raw.(core.EvaluationResult)
		if !ok {
			continue
		}
		totalLatency += res.LatencyMS
		wt := res.Score.Weight
		if wt == 0 {
			wt = 1.0
		}
		weightedSum += res.Score.Normalized * wt
		totalWeight += wt
		count++
	}

	avgLatency := int64(0)
	if count > 0 {
		avgLatency = totalLatency / int64(count)
	}
	overallScore := 0.0
	if totalWeight > 0 {
		overallScore = round4(weightedSum / totalWeight)
	}

	return storage.Telemetry{
		AvgLatencyMS: avgLatency,
		OverallScore: overallScore,
	}
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
func round4(f float64) float64 { return math.Round(f*10000) / 10000 }
