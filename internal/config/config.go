// Package config handles loading and validating the caliper.yml configuration
// file. All structs in this package map 1-to-1 to YAML fields; no business
// logic lives here.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Top-level config
// ---------------------------------------------------------------------------

// Config is the root struct that maps the entire caliper.yml file.
type Config struct {
	// Version is a semver string used for forward-compatibility checks
	// (e.g. "1.0"). The CLI will reject configs whose major version exceeds
	// its own supported maximum.
	Version string `yaml:"version"`

	// Imports is a list of relative or absolute glob patterns pointing to
	// modular YAML configuration files (e.g., "datasets/*.yml").
	Imports []string `yaml:"imports,omitempty"`

	// Profiles is the ordered list of named provider configurations.
	// Each DatasetGroup references a Profile by name.
	Profiles []Profile `yaml:"profiles"`

	// Datasets is the ordered list of dataset groups to evaluate.
	Datasets []DatasetGroup `yaml:"datasets"`

	// Reporters configures which output adapters the CLI will call after
	// the DAG finishes. If omitted, the console reporter is used by default.
	Reporters []ReporterConfig `yaml:"reporters,omitempty"`
}

// ---------------------------------------------------------------------------
// Profile & ProviderConfig
// ---------------------------------------------------------------------------

// Profile is a named, reusable LLM provider configuration.
// Multiple DatasetGroups may share the same profile.
type Profile struct {
	// Name is the unique identifier referenced by DatasetGroup.Profile.
	Name string `yaml:"name"`

	// Provider holds the connection settings for the LLM backend.
	Provider ProviderConfig `yaml:"provider"`
}

// ProviderConfig contains the settings required to construct a Provider
// implementation at runtime.
type ProviderConfig struct {
	// Type selects the Provider implementation.
	// Built-in values: "openai", "anthropic", "mock".
	// Third-party plugins may register additional types.
	Type string `yaml:"type"`

	// Model is the model identifier forwarded to the backend API
	// (e.g. "gpt-4o", "claude-3-5-sonnet-20241022").
	// Ignored by the "mock" provider.
	Model string `yaml:"model,omitempty"`

	// APIKeyEnv is the name of the environment variable that holds the
	// API key. The CLI reads os.Getenv(APIKeyEnv) at runtime so that
	// secrets are never stored in the config file.
	APIKeyEnv string `yaml:"api_key_env,omitempty"`

	// TimeoutMS is the per-request deadline in milliseconds.
	// Defaults to 30 000 ms (30 s) if zero.
	TimeoutMS int `yaml:"timeout_ms,omitempty"`

	// Params is an open-ended map for provider-specific options
	// (e.g. temperature, max_tokens, base_url for self-hosted models).
	Params map[string]any `yaml:"params,omitempty"`
}

// ---------------------------------------------------------------------------
// DatasetGroup & TestCase
// ---------------------------------------------------------------------------

// DatasetGroup is a cohesive collection of TestCases evaluated against a
// single Profile. It owns the DAG of EvaluatorConfigs that are applied to
// every test case in the group.
type DatasetGroup struct {
	// ID is the stable, machine-readable identifier for this group.
	// Referenced by the regression engine (Phase 2) when looking up baselines.
	ID string `yaml:"id"`

	// Name is a human-readable label shown in reports.
	Name string `yaml:"name"`

	// Profile references a Profile.Name defined in Config.Profiles.
	Profile string `yaml:"profile"`

	// TestCases is the ordered list of prompt/expected-output pairs.
	TestCases []TestCase `yaml:"test_cases"`

	// Evaluators defines the DAG of evaluation nodes applied to each
	// test case in this group.
	Evaluators []EvaluatorConfig `yaml:"evaluators"`

	// Budget caps spending and latency for this dataset group per run.
	// All fields are optional; a zero value means "uncapped".
	Budget Budget `yaml:"budget,omitempty"`

	// Regression holds the thresholds compared against the most-recent
	// successful baseline by the RegressionEvaluator node.
	Regression RegressionConfig `yaml:"regression,omitempty"`
}

// ---------------------------------------------------------------------------
// Budget
// ---------------------------------------------------------------------------

// Budget caps the maximum allowed spend and latency for a single evaluate
// invocation on a DatasetGroup. Enforced by the evaluate command after the
// DAG run; a breach sets the exit code to 1 even if all evaluators passed.
type Budget struct {
	// MaxCostUSD is the maximum total provider cost in US dollars for one
	// run across all test cases in the group. 0 means uncapped.
	MaxCostUSD float64 `yaml:"max_cost_usd,omitempty"`

	// MaxLatencyMS is the maximum allowed average latency in milliseconds
	// across all test cases in the group. 0 means uncapped.
	MaxLatencyMS int64 `yaml:"max_latency_ms,omitempty"`
}

// ---------------------------------------------------------------------------
// RegressionConfig
// ---------------------------------------------------------------------------

// RegressionConfig holds the thresholds used by the RegressionEvaluator node.
// All fields are optional; a zero value means "no threshold for this metric".
type RegressionConfig struct {
	// MaxLatencyRegressionPct is the maximum allowed percentage increase in
	// average latency compared to the baseline. E.g. 20 = allow up to 20% slower.
	MaxLatencyRegressionPct float64 `yaml:"max_latency_regression_pct,omitempty"`

	// MaxCostRegressionUSD is the maximum allowed absolute increase in cost
	// (USD) compared to the baseline. E.g. 0.005 = allow up to half a cent more.
	MaxCostRegressionUSD float64 `yaml:"max_cost_regression_usd,omitempty"`

	// MaxScoreRegressionPct is the maximum allowed percentage *decrease* in
	// the weighted overall score compared to the baseline. E.g. 10 = allow up
	// to a 10% score drop (from 1.0 to 0.90 would be exactly 10%).
	MaxScoreRegressionPct float64 `yaml:"max_score_regression_pct,omitempty"`
}

// TestCase is the atomic unit of evaluation: a single prompt sent to the
// provider and assessed by the evaluator DAG.
type TestCase struct {
	// ID is a stable, unique identifier within the parent DatasetGroup.
	ID string `yaml:"id"`

	// Prompt is the raw text forwarded to the Provider.Complete call.
	Prompt string `yaml:"prompt"`

	// Expected is the optional reference answer used by exact-match or
	// semantic evaluators. Evaluators that do not need a reference output
	// (e.g. RegexEvaluator with a self-contained pattern) may leave this empty.
	Expected string `yaml:"expected,omitempty"`

	// Tags are arbitrary labels for filtering test cases at run time
	// (e.g. --filter-tag=regression, --filter-tag=smoke).
	Tags []string `yaml:"tags,omitempty"`
}

// ---------------------------------------------------------------------------
// EvaluatorConfig
// ---------------------------------------------------------------------------

// EvaluatorConfig is the YAML representation of a single node in the
// evaluation DAG. At runtime the engine resolves the Type field to a
// concrete Evaluator implementation and wires up the dependency graph
// from DependsOn.
type EvaluatorConfig struct {
	// ID is the unique node identifier within the parent DatasetGroup's DAG.
	// Referenced by other nodes' DependsOn slices.
	ID string `yaml:"id"`

	// Type selects the Evaluator implementation.
	// Built-in values: "regex", "mock".
	// Phase 2 adds: "regression".
	Type string `yaml:"type"`

	// DependsOn lists the IDs of EvaluatorConfig nodes that must succeed
	// before this node is executed. An empty slice marks a root node.
	DependsOn []string `yaml:"depends_on,omitempty"`

	// Weight is fed directly into core.Score.Weight for this evaluator.
	// A value of 0 is normalised to 1.0 by the engine at startup.
	Weight float64 `yaml:"weight,omitempty"`

	// Params is an open-ended map of evaluator-specific configuration
	// (e.g. {"pattern": "Paris"} for a regex evaluator).
	Params map[string]any `yaml:"params,omitempty"`
}

// ---------------------------------------------------------------------------
// ReporterConfig
// ---------------------------------------------------------------------------

// ReporterConfig selects and parameterises a Reporter implementation.
type ReporterConfig struct {
	// Type selects the Reporter implementation.
	// Built-in values: "console", "json".
	Type string `yaml:"type"`

	// Params is an open-ended map of reporter-specific options
	// (e.g. {"output": "./results.json"} for the JSON reporter).
	Params map[string]any `yaml:"params,omitempty"`
}

// ---------------------------------------------------------------------------
// Load & Validate
// ---------------------------------------------------------------------------

// Load reads the file at path, unmarshals it as YAML into a Config, and
// calls Validate before returning. It is the primary entry point for the
// evaluate command.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: cannot read file %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: cannot parse YAML in %q: %w", path, err)
	}

	baseDir := filepath.Dir(path)
	for _, imp := range cfg.Imports {
		pattern := imp
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(baseDir, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("config: import pattern %q invalid: %w", imp, err)
		}
		for _, match := range matches {
			if match == path {
				continue
			}
			impData, err := os.ReadFile(match)
			if err != nil {
				return nil, fmt.Errorf("config: cannot read imported file %q: %w", match, err)
			}
			var subCfg Config
			if err := yaml.Unmarshal(impData, &subCfg); err != nil {
				return nil, fmt.Errorf("config: cannot parse imported YAML in %q: %w", match, err)
			}
			cfg.Profiles = append(cfg.Profiles, subCfg.Profiles...)
			cfg.Datasets = append(cfg.Datasets, subCfg.Datasets...)
			cfg.Reporters = append(cfg.Reporters, subCfg.Reporters...)
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: validation failed: %w", err)
	}

	return &cfg, nil
}

// Validate performs structural checks on the config and returns a joined
// error describing every violation found (not just the first). This lets
// users fix all mistakes in a single edit cycle.
func (c *Config) Validate() error {
	var errs []error

	if c.Version == "" {
		errs = append(errs, errors.New("version is required"))
	}

	// Build a profile name index for O(1) lookup.
	profileNames := make(map[string]struct{}, len(c.Profiles))
	for i, p := range c.Profiles {
		if p.Name == "" {
			errs = append(errs, fmt.Errorf("profiles[%d]: name is required", i))
			continue
		}
		if _, dup := profileNames[p.Name]; dup {
			errs = append(errs, fmt.Errorf("profiles[%d]: duplicate name %q", i, p.Name))
		}
		profileNames[p.Name] = struct{}{}

		if p.Provider.Type == "" {
			errs = append(errs, fmt.Errorf("profiles[%d] (%s): provider.type is required", i, p.Name))
		}
	}

	if len(c.Datasets) == 0 {
		errs = append(errs, errors.New("at least one dataset group is required"))
	}

	// Validate each dataset group.
	datasetIDs := make(map[string]struct{}, len(c.Datasets))
	for i, ds := range c.Datasets {
		prefix := fmt.Sprintf("datasets[%d]", i)

		if ds.ID == "" {
			errs = append(errs, fmt.Errorf("%s: id is required", prefix))
		} else {
			if _, dup := datasetIDs[ds.ID]; dup {
				errs = append(errs, fmt.Errorf("%s: duplicate id %q", prefix, ds.ID))
			}
			datasetIDs[ds.ID] = struct{}{}
			prefix = fmt.Sprintf("datasets[%s]", ds.ID)
		}

		if ds.Profile == "" {
			errs = append(errs, fmt.Errorf("%s: profile reference is required", prefix))
		} else if _, ok := profileNames[ds.Profile]; !ok {
			errs = append(errs, fmt.Errorf("%s: profile %q is not defined", prefix, ds.Profile))
		}

		if len(ds.TestCases) == 0 {
			errs = append(errs, fmt.Errorf("%s: at least one test_case is required", prefix))
		}

		for j, tc := range ds.TestCases {
			if tc.ID == "" {
				errs = append(errs, fmt.Errorf("%s.test_cases[%d]: id is required", prefix, j))
			}
			if tc.Prompt == "" {
				errs = append(errs, fmt.Errorf("%s.test_cases[%d] (%s): prompt is required", prefix, j, tc.ID))
			}
		}

		errs = append(errs, validateEvaluatorDAG(prefix, ds.Evaluators)...)
	}

	return errors.Join(errs...)
}

// validateEvaluatorDAG checks that evaluator IDs are unique and that all
// DependsOn references resolve to a declared evaluator within the same group.
func validateEvaluatorDAG(prefix string, evals []EvaluatorConfig) []error {
	if len(evals) == 0 {
		return []error{fmt.Errorf("%s: at least one evaluator is required", prefix)}
	}

	var errs []error
	ids := make(map[string]struct{}, len(evals))

	for i, ev := range evals {
		evPrefix := fmt.Sprintf("%s.evaluators[%d]", prefix, i)

		if ev.ID == "" {
			errs = append(errs, fmt.Errorf("%s: id is required", evPrefix))
			continue
		}
		if _, dup := ids[ev.ID]; dup {
			errs = append(errs, fmt.Errorf("%s: duplicate evaluator id %q", evPrefix, ev.ID))
		}
		ids[ev.ID] = struct{}{}

		if ev.Type == "" {
			errs = append(errs, fmt.Errorf("%s (%s): type is required", evPrefix, ev.ID))
		}
	}

	// Second pass: validate depends_on references now that ids is fully built.
	for i, ev := range evals {
		evPrefix := fmt.Sprintf("%s.evaluators[%s]", prefix, ev.ID)
		for _, dep := range ev.DependsOn {
			if _, ok := ids[dep]; !ok {
				errs = append(errs, fmt.Errorf(
					"%s: depends_on references unknown evaluator %q (evaluators[%d])",
					evPrefix, dep, i,
				))
			}
		}
	}

	return errs
}
