package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/pranavkakde/caliper/internal/config"
	"github.com/pranavkakde/caliper/internal/engine"
	"github.com/pranavkakde/caliper/internal/evaluation"
	"github.com/pranavkakde/caliper/internal/evaluator"
	"github.com/pranavkakde/caliper/internal/provider"
	"github.com/pranavkakde/caliper/internal/reporter"
	"github.com/pranavkakde/caliper/internal/storage"
	"github.com/pranavkakde/caliper/pkg/core"
	"github.com/spf13/cobra"
)

// evaluateCmd runs the evaluation DAG against the configured provider.
var evaluateCmd = &cobra.Command{
	Use:   "evaluate",
	Short: "Run the evaluation DAG defined in the config file",
	Long: `evaluate parses the caliper YAML configuration, resolves evaluator
dependencies into a Directed Acyclic Graph (DAG), and executes the nodes
concurrently against the configured provider.

Results are written to the local history store (--history-dir) after each
dataset group run so they can be used as a regression baseline on the next run.

Exit codes:
  0  all evaluators passed and all budget/regression checks passed
  1  one or more evaluators failed, or a budget/regression threshold was exceeded`,
	// SilenceUsage prevents cobra from printing the usage block when RunE
	// returns an error. Usage text is only helpful for flag/arg mistakes,
	// not for runtime evaluation failures which the reporter already surfaces.
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, err := cmd.Flags().GetString("config")
		if err != nil {
			return fmt.Errorf("failed to read --config flag: %w", err)
		}

		if verbose {
			fmt.Fprintf(os.Stderr, "[caliper] loading config from: %s\n", configPath)
			fmt.Fprintf(os.Stderr, "[caliper] history dir: %s\n", historyDir)
		}

		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}

		if verbose {
			fmt.Fprintf(os.Stderr,
				"[caliper] config v%s loaded — %d profile(s), %d dataset group(s)\n",
				cfg.Version, len(cfg.Profiles), len(cfg.Datasets),
			)
		}

		return runEvaluate(cfg, historyDir, verbose)
	},
}

// runEvaluate is the core evaluation loop extracted from RunE for testability.
func runEvaluate(cfg *config.Config, histDir string, verbose bool) error {
	ctx := context.Background()

	// ── Storage adapter ───────────────────────────────────────────────
	store := storage.NewLocalStorage(histDir)

	// ── Build reporters ───────────────────────────────────────────────
	reporters, err := reporter.Build(cfg.Reporters)
	if err != nil {
		return fmt.Errorf("reporter setup: %w", err)
	}

	// ── Index profiles for O(1) lookup ────────────────────────────────
	profileIndex := make(map[string]config.Profile, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		profileIndex[p.Name] = p
	}

	// ── Accumulate results across all dataset groups and test cases ───
	var allResults []core.EvaluationResult
	overallPassed := true

	for _, ds := range cfg.Datasets {
		if verbose {
			fmt.Fprintf(os.Stderr, "[caliper] dataset: %s (%s)\n", ds.Name, ds.ID)
		}

		// Resolve profile → provider.
		prof, ok := profileIndex[ds.Profile]
		if !ok {
			return fmt.Errorf("dataset %q references unknown profile %q", ds.ID, ds.Profile)
		}

		prov, err := provider.Build(prof.Provider)
		if err != nil {
			return fmt.Errorf("dataset %q: %w", ds.ID, err)
		}

		// ── Phase 2: load baseline before building evaluators ─────────
		// If no regression evaluator is configured this is a cheap no-op
		// (LoadLatestBaseline returns quickly on glob miss).
		baseline, err := store.LoadLatestBaseline(ds.ID)
		if err != nil {
			// Non-fatal: log and continue without a baseline.
			fmt.Fprintf(os.Stderr,
				"[caliper] warning: could not load baseline for %q: %v\n", ds.ID, err)
		}
		if verbose {
			if baseline != nil {
				fmt.Fprintf(os.Stderr,
					"[caliper]   baseline: run=%s score=%.2f avgLatency=%dms\n",
					baseline.RunID, baseline.Telemetry.OverallScore, baseline.Telemetry.AvgLatencyMS,
				)
			} else {
				fmt.Fprintf(os.Stderr, "[caliper]   baseline: none (first run)\n")
			}
		}

		// Build evaluators with baseline injected for regression nodes.
		opts := evaluator.BuildOptions{
			Baseline:      baseline,
			RegressionCfg: ds.Regression,
		}
		evaluators, err := evaluator.Build(ds.Evaluators, opts)
		if err != nil {
			return fmt.Errorf("dataset %q: evaluator setup: %w", ds.ID, err)
		}

		dag, err := engine.New(evaluators)
		if err != nil {
			return fmt.Errorf("dataset %q: DAG construction: %w", ds.ID, err)
		}

		if verbose {
			fmt.Fprintf(os.Stderr,
				"[caliper]   provider=%s  evaluators=%d  test-cases=%d\n",
				prov.Name(), dag.NodeCount(), len(ds.TestCases),
			)
		}

		// ── Per-test-case execution ───────────────────────────────────
		var dsResults []core.EvaluationResult

		for _, tc := range ds.TestCases {
			runID := fmt.Sprintf("%s-%s-%d", ds.ID, tc.ID, time.Now().UnixNano())
			ec := evaluation.NewRunContext(runID, prov)

			// Call the provider exactly once per test case.
			response, err := prov.Complete(ctx, tc.Prompt)
			if err != nil {
				return fmt.Errorf("dataset %q, test case %q: provider.Complete failed: %w",
					ds.ID, tc.ID, err)
			}

			ec.Set("prompt", tc.Prompt)
			ec.Set("response", response)
			ec.Set("cost_usd", 0.0) // Phase 3+: real providers set actual cost
			if tc.Expected != "" {
				ec.Set("expected", tc.Expected)
			}

			if verbose {
				fmt.Fprintf(os.Stderr, "[caliper]   running test case: %s\n", tc.ID)
			}

			results, err := dag.Run(ctx, ec)
			if err != nil {
				return fmt.Errorf("dataset %q, test case %q: DAG run failed: %w",
					ds.ID, tc.ID, err)
			}

			dsResults = append(dsResults, results...)
		}

		// ── Phase 2: compute telemetry and persist run record ─────────
		tel := storage.ComputeTelemetry(dsResults, 0.0)
		record := storage.RunRecord{
			RunID:         fmt.Sprintf("%s-%d", ds.ID, time.Now().UnixNano()),
			Timestamp:     time.Now().UTC(),
			DatasetID:     ds.ID,
			ConfigVersion: cfg.Version,
			Synced:        false,
			Telemetry:     tel,
			Results:       storage.ToResultRecords(dsResults),
		}
		if err := store.Save(record); err != nil {
			// Non-fatal: warn but do not abort the run.
			fmt.Fprintf(os.Stderr,
				"[caliper] warning: failed to save history for %q: %v\n", ds.ID, err)
		} else if verbose {
			fmt.Fprintf(os.Stderr,
				"[caliper]   saved: score=%.2f passed=%v → %s/history/\n",
				tel.OverallScore, tel.Passed, histDir,
			)
		}

		// ── Phase 2: enforce budget thresholds ────────────────────────
		if ds.Budget.MaxLatencyMS > 0 && tel.AvgLatencyMS > ds.Budget.MaxLatencyMS {
			fmt.Fprintf(os.Stderr,
				"[caliper] BUDGET EXCEEDED for %q: avg latency %dms > limit %dms\n",
				ds.ID, tel.AvgLatencyMS, ds.Budget.MaxLatencyMS,
			)
			overallPassed = false
		}
		if ds.Budget.MaxCostUSD > 0 && tel.CostUSD > ds.Budget.MaxCostUSD {
			fmt.Fprintf(os.Stderr,
				"[caliper] BUDGET EXCEEDED for %q: cost $%.4f > limit $%.4f\n",
				ds.ID, tel.CostUSD, ds.Budget.MaxCostUSD,
			)
			overallPassed = false
		}

		allResults = append(allResults, dsResults...)
	}

	// ── Report ────────────────────────────────────────────────────────
	for _, r := range reporters {
		if err := r.Report(ctx, allResults); err != nil {
			fmt.Fprintf(os.Stderr, "[caliper] reporter error: %v\n", err)
		}
	}

	// ── Exit code ─────────────────────────────────────────────────────
	if !engine.AllPassed(allResults) || !overallPassed {
		return fmt.Errorf("evaluation failed: one or more evaluators or budget checks did not pass")
	}

	return nil
}
