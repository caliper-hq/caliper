// Package storage implements the offline-first local history layer for Caliper.
// Evaluation results are persisted as date-sharded JSON files under a root
// directory (typically `.caliper/`), enabling baseline retrieval and the
// regression engine introduced in Phase 2.
package storage

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pranavkakde/caliper/pkg/core"
)

// ---------------------------------------------------------------------------
// On-disk record types
// ---------------------------------------------------------------------------

// ResultRecord is the JSON-serialisable mirror of core.EvaluationResult.
// It replaces the non-serialisable `error` interface with a plain string so
// the full result set can round-trip through JSON without data loss.
type ResultRecord struct {
	EvaluatorID string         `json:"evaluator_id"`
	Passed      bool           `json:"passed"`
	Score       core.Score     `json:"score"`
	LatencyMS   int64          `json:"latency_ms"`
	Details     map[string]any `json:"details,omitempty"`
	ErrMessage  string         `json:"error,omitempty"` // core.EvaluationResult.Err.Error()
}

// Telemetry holds the aggregate statistics for one evaluate invocation on a
// single DatasetGroup. It is computed from the full result set once all test
// cases in the group have run, and stored inside RunRecord.
type Telemetry struct {
	// AvgLatencyMS is the mean per-evaluator latency across all test cases.
	AvgLatencyMS int64 `json:"avg_latency_ms"`

	// TotalLatencyMS is the sum of all evaluator latencies across all test cases.
	TotalLatencyMS int64 `json:"total_latency_ms"`

	// CostUSD is the total provider cost for this run in US dollars.
	// Always 0.0 for the mock provider; populated by real providers in Phase 3+.
	CostUSD float64 `json:"cost_usd"`

	// OverallScore is the weighted average of all passing evaluators' normalised
	// scores across all test cases (skipped/failed nodes contribute 0).
	OverallScore float64 `json:"overall_score"`

	// Passed is true iff every non-skipped evaluator across all test cases passed.
	Passed bool `json:"passed"`

	// Counts
	EvalCount    int `json:"eval_count"`
	PassedCount  int `json:"passed_count"`
	FailedCount  int `json:"failed_count"`
	SkippedCount int `json:"skipped_count"`
}

// RunRecord is the top-level document written to disk for each evaluate
// invocation on a single DatasetGroup. One file = one dataset group run.
type RunRecord struct {
	// RunID is a unique identifier of the form "datasetID-unixNano".
	RunID string `json:"run_id"`

	// Timestamp is when the evaluation finished (UTC).
	Timestamp time.Time `json:"timestamp"`

	// DatasetID matches the DatasetGroup.ID from the YAML config.
	DatasetID string `json:"dataset_id"`

	// ConfigVersion is the `version` field from the caliper.yml that produced this run.
	ConfigVersion string `json:"config_version"`

	// Synced is false until `caliper sync` (Phase 3) uploads this record to
	// the NestJS control plane and marks it as synced.
	Synced bool `json:"synced"`

	// Telemetry holds the aggregate statistics for this run.
	Telemetry Telemetry `json:"telemetry"`

	// Results contains one ResultRecord per evaluator per test case, in
	// topological + test-case order (matching engine.DAGEngine output order).
	Results []ResultRecord `json:"results"`
}

// ---------------------------------------------------------------------------
// ComputeTelemetry — pure helper used by evaluate.go
// ---------------------------------------------------------------------------

// ComputeTelemetry derives a Telemetry value from a flat slice of
// EvaluationResults (all test cases in the group, flattened) and the total
// provider cost for the run.
//
// It is a pure function with no side effects, making it easy to test.
func ComputeTelemetry(results []core.EvaluationResult, costUSD float64) Telemetry {
	var (
		totalLatency            int64
		weightedSum             float64
		totalWeight             float64
		passed, failed, skipped int
	)

	isSkipped := func(r core.EvaluationResult) bool {
		_, ok := r.Details["skipped_reason"]
		return !r.Passed && ok
	}

	for _, r := range results {
		totalLatency += r.LatencyMS
		switch {
		case r.Passed:
			passed++
			wt := r.Score.Weight
			if wt == 0 {
				wt = 1.0
			}
			weightedSum += r.Score.Normalized * wt
			totalWeight += wt
		case isSkipped(r):
			skipped++
		default:
			failed++
			wt := r.Score.Weight
			if wt == 0 {
				wt = 1.0
			}
			totalWeight += wt // still counts toward denominator
		}
	}

	var avgLatency int64
	if len(results) > 0 {
		avgLatency = totalLatency / int64(len(results))
	}

	overallScore := 0.0
	if totalWeight > 0 {
		overallScore = math.Round(weightedSum/totalWeight*10000) / 10000
	}

	return Telemetry{
		AvgLatencyMS:   avgLatency,
		TotalLatencyMS: totalLatency,
		CostUSD:        costUSD,
		OverallScore:   overallScore,
		Passed:         failed == 0 && len(results) > 0,
		EvalCount:      len(results),
		PassedCount:    passed,
		FailedCount:    failed,
		SkippedCount:   skipped,
	}
}

// ToResultRecords converts a slice of core.EvaluationResult to the
// JSON-serialisable ResultRecord representation.
func ToResultRecords(results []core.EvaluationResult) []ResultRecord {
	out := make([]ResultRecord, len(results))
	for i, r := range results {
		rec := ResultRecord{
			EvaluatorID: r.EvaluatorID,
			Passed:      r.Passed,
			Score:       r.Score,
			LatencyMS:   r.LatencyMS,
			Details:     r.Details,
		}
		if r.Err != nil {
			rec.ErrMessage = r.Err.Error()
		}
		out[i] = rec
	}
	return out
}

// ---------------------------------------------------------------------------
// LocalStorage
// ---------------------------------------------------------------------------

// LocalStorage persists RunRecords under a root directory using the layout:
//
//	{rootDir}/history/YYYY/MM/run-{datasetID}-{unixNano}.json
type LocalStorage struct {
	rootDir string
}

// NewLocalStorage returns a LocalStorage rooted at rootDir (e.g. ".caliper").
func NewLocalStorage(rootDir string) *LocalStorage {
	return &LocalStorage{rootDir: rootDir}
}

// historyDir returns the absolute path to the history subdirectory.
func (s *LocalStorage) historyDir() string {
	return filepath.Join(s.rootDir, "history")
}

// runPath constructs the full file path for a RunRecord based on its
// timestamp and dataset ID.
func (s *LocalStorage) runPath(t time.Time, datasetID string, nanos int64) string {
	year := t.UTC().Format("2006")
	month := t.UTC().Format("01")
	filename := fmt.Sprintf("run-%s-%d.json", sanitise(datasetID), nanos)
	return filepath.Join(s.historyDir(), year, month, filename)
}

// sanitise replaces characters that are unsafe in filenames with hyphens.
func sanitise(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}

// Save marshals record to JSON and writes it atomically to the date-sharded
// path. "Atomic" here means: write to a sibling .tmp file first, then
// os.Rename — so a partially-written file is never visible to readers.
func (s *LocalStorage) Save(record RunRecord) error {
	nanos := record.Timestamp.UnixNano()
	path := s.runPath(record.Timestamp, record.DatasetID, nanos)

	// Ensure the directory tree exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("storage: failed to create history directory %q: %w", dir, err)
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("storage: failed to marshal run record: %w", err)
	}

	// Write to a temp file in the same directory, then rename atomically.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("storage: failed to write temp file %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		// Clean up the orphaned temp file on failure.
		_ = os.Remove(tmpPath)
		return fmt.Errorf("storage: failed to rename %q → %q: %w", tmpPath, path, err)
	}

	return nil
}

// LoadLatestBaseline scans the history directory for all RunRecords belonging
// to datasetID, and returns the most-recent one where Telemetry.Passed == true.
//
// Returns (nil, nil) if no qualifying baseline exists yet — callers should
// treat this as "first run; no baseline available".
//
// The scan is O(N) in the number of history files. For large histories
// Phase 3 will introduce an index; for Phase 2 the file count is small.
func (s *LocalStorage) LoadLatestBaseline(datasetID string) (*RunRecord, error) {
	pattern := filepath.Join(s.historyDir(), "*", "*",
		fmt.Sprintf("run-%s-*.json", sanitise(datasetID)))

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("storage: glob error: %w", err)
	}
	if len(matches) == 0 {
		return nil, nil // no history yet
	}

	// Sort descending by filename (which encodes unixNano) so the first
	// qualifying record we parse is the most recent.
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))

	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			// Skip unreadable files (e.g. partial writes) rather than aborting.
			continue
		}
		var record RunRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue // skip corrupt files
		}
		if record.DatasetID == datasetID && record.Telemetry.Passed {
			return &record, nil
		}
	}

	return nil, nil // no successful baseline found
}



// StoredRun pairs a persisted record with its local history file. The path is
// intentionally exposed so a caller can mark precisely the records it has
// successfully delivered to a remote control plane.
type StoredRun struct {
	Path   string
	Record RunRecord
}

// LoadUnsynced returns every valid local run that has not yet been uploaded.
// Corrupt history files are ignored: a malformed local record must not prevent
// healthy runs from being synced.
func (s *LocalStorage) LoadUnsynced() ([]StoredRun, error) {
	pattern := filepath.Join(s.historyDir(), "*", "*", "run-*.json")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("storage: glob history: %w", err)
	}
	sort.Strings(paths)
	runs := make([]StoredRun, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var record RunRecord
		if json.Unmarshal(data, &record) != nil || record.RunID == "" || record.Synced {
			continue
		}
		runs = append(runs, StoredRun{Path: path, Record: record})
	}
	return runs, nil
}

// MarkSynced atomically updates the supplied history records after the API has
// accepted them. A file is never marked before a successful response.
func (s *LocalStorage) MarkSynced(runs []StoredRun) error {
	for _, run := range runs {
		data, err := os.ReadFile(run.Path)
		if err != nil {
			return fmt.Errorf("storage: read %q: %w", run.Path, err)
		}
		var record RunRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return fmt.Errorf("storage: parse %q: %w", run.Path, err)
		}
		record.Synced = true
		updated, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			return fmt.Errorf("storage: marshal %q: %w", run.Path, err)
		}
		tmp := run.Path + ".tmp"
		if err := os.WriteFile(tmp, updated, 0o644); err != nil {
			return fmt.Errorf("storage: write %q: %w", tmp, err)
		}
		if err := os.Rename(tmp, run.Path); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("storage: replace %q: %w", run.Path, err)
		}
	}
	return nil
}
