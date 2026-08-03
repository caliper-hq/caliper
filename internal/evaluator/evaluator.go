// Package evaluator contains concrete implementations of core.Evaluator and
// a factory that converts config.EvaluatorConfig slices into live evaluators.
package evaluator

import (
	"context"
	"fmt"
	"regexp"

	"github.com/pranavkakde/caliper/internal/config"
	"github.com/pranavkakde/caliper/internal/storage"
	"github.com/pranavkakde/caliper/pkg/core"
)

// ---------------------------------------------------------------------------
// RegexEvaluator
// ---------------------------------------------------------------------------

// RegexEvaluator is a DAG node that passes when the LLM response stored in
// EvaluationContext under the "response" key matches a compiled regular
// expression pattern.
//
// Design choice: RegexEvaluator reads the response from context rather than
// calling the provider itself. This allows the evaluate command to call the
// provider exactly once per test case (storing the result as "response"),
// while any number of regex nodes share that single response without
// additional API calls.
type RegexEvaluator struct {
	id      string
	deps    []string
	pattern *regexp.Regexp
	weight  float64
}

// NewRegex constructs a RegexEvaluator. Returns an error if pattern is not a
// valid regular expression. weight=0 is normalised to 1.0.
func NewRegex(id string, deps []string, pattern string, weight float64) (*RegexEvaluator, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("evaluator %q: invalid regex pattern %q: %w", id, pattern, err)
	}
	if weight == 0 {
		weight = 1.0
	}
	return &RegexEvaluator{id: id, deps: deps, pattern: re, weight: weight}, nil
}

// ID implements core.Evaluator.
func (r *RegexEvaluator) ID() string { return r.id }

// Dependencies implements core.Evaluator.
func (r *RegexEvaluator) Dependencies() []string { return r.deps }

// Evaluate implements core.Evaluator. It reads "response" from ec, applies
// the compiled pattern, and returns a result with full diagnostic details.
func (r *RegexEvaluator) Evaluate(_ context.Context, ec core.EvaluationContext) (core.EvaluationResult, error) {
	raw, ok := ec.Get("response")
	if !ok {
		return core.EvaluationResult{}, fmt.Errorf(
			"evaluator %q: key \"response\" not found in EvaluationContext — "+
				"ensure the evaluate command calls provider.Complete before running the DAG",
			r.id,
		)
	}
	response, ok := raw.(string)
	if !ok {
		return core.EvaluationResult{}, fmt.Errorf(
			"evaluator %q: \"response\" in context is %T, expected string",
			r.id, raw,
		)
	}

	matched := r.pattern.MatchString(response)

	rawScore := 0.0
	if matched {
		rawScore = 1.0
	}

	return core.EvaluationResult{
		EvaluatorID: r.id,
		Passed:      matched,
		Score: core.Score{
			Raw:        rawScore,
			Normalized: rawScore,
			Weight:     r.weight,
		},
		Details: map[string]any{
			"pattern":          r.pattern.String(),
			"matched":          matched,
			"response_snippet": truncate(response, 120),
		},
	}, nil
}

// truncate returns s unchanged if len(s) <= max, otherwise returns the first
// max bytes followed by an ellipsis. Used to keep Details readable.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// BuildOptions carries optional context the factory needs to construct
// evaluator types that depend on external state (e.g. the regression baseline).
type BuildOptions struct {
	// Baseline is the most-recent successful RunRecord for the DatasetGroup
	// being evaluated. Nil means no baseline exists yet (first run).
	Baseline *storage.RunRecord

	// RegressionCfg holds the thresholds injected into RegressionEvaluator
	// nodes. Populated from DatasetGroup.Regression in the YAML.
	RegressionCfg config.RegressionConfig
}

// Build converts a slice of EvaluatorConfig structs (from the parsed YAML)
// into concrete core.Evaluator implementations ready to pass to engine.New.
// Returns an error if any config is invalid or references an unsupported type.
func Build(cfgs []config.EvaluatorConfig, opts BuildOptions) ([]core.Evaluator, error) {
	evs := make([]core.Evaluator, 0, len(cfgs))
	for _, cfg := range cfgs {
		ev, err := buildOne(cfg, opts)
		if err != nil {
			return nil, err
		}
		evs = append(evs, ev)
	}
	return evs, nil
}

// buildOne dispatches to the appropriate constructor based on cfg.Type.
func buildOne(cfg config.EvaluatorConfig, opts BuildOptions) (core.Evaluator, error) {
	weight := cfg.Weight
	if weight == 0 {
		weight = 1.0
	}

	switch cfg.Type {
	case "regex":
		pattern, _ := cfg.Params["pattern"].(string)
		if pattern == "" {
			return nil, fmt.Errorf(
				"evaluator %q (type=regex): params.pattern is required and must be a non-empty string",
				cfg.ID,
			)
		}
		return NewRegex(cfg.ID, cfg.DependsOn, pattern, weight)

	case "regression":
		// Override thresholds from params if specified inline; otherwise fall
		// back to the DatasetGroup-level RegressionConfig from opts.
		thresholds := opts.RegressionCfg
		if v, ok := cfg.Params["max_latency_regression_pct"].(float64); ok {
			thresholds.MaxLatencyRegressionPct = v
		}
		if v, ok := cfg.Params["max_score_regression_pct"].(float64); ok {
			thresholds.MaxScoreRegressionPct = v
		}
		if v, ok := cfg.Params["max_cost_regression_usd"].(float64); ok {
			thresholds.MaxCostRegressionUSD = v
		}
		return newRegression(cfg.ID, cfg.DependsOn, opts.Baseline, thresholds), nil

	default:
		return nil, fmt.Errorf(
			"evaluator %q: unknown type %q (supported types: \"regex\", \"regression\")",
			cfg.ID, cfg.Type,
		)
	}
}
