package storage_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pranavkakde/caliper/internal/storage"
	"github.com/pranavkakde/caliper/pkg/core"
)

// ---------------------------------------------------------------------------
// ComputeTelemetry
// ---------------------------------------------------------------------------

func TestComputeTelemetry_AllPass(t *testing.T) {
	results := []core.EvaluationResult{
		{Passed: true, LatencyMS: 10, Score: core.Score{Normalized: 1.0, Weight: 1.0}},
		{Passed: true, LatencyMS: 20, Score: core.Score{Normalized: 0.8, Weight: 2.0}},
	}
	tel := storage.ComputeTelemetry(results, 0.0)

	if !tel.Passed {
		t.Error("expected Passed==true")
	}
	if tel.PassedCount != 2 {
		t.Errorf("expected PassedCount=2, got %d", tel.PassedCount)
	}
	if tel.FailedCount != 0 {
		t.Errorf("expected FailedCount=0, got %d", tel.FailedCount)
	}
	if tel.AvgLatencyMS != 15 {
		t.Errorf("expected AvgLatencyMS=15, got %d", tel.AvgLatencyMS)
	}
	if tel.TotalLatencyMS != 30 {
		t.Errorf("expected TotalLatencyMS=30, got %d", tel.TotalLatencyMS)
	}
	// Weighted score: (1.0*1 + 0.8*2) / (1+2) = 2.6/3 ≈ 0.8667
	if tel.OverallScore < 0.86 || tel.OverallScore > 0.87 {
		t.Errorf("expected OverallScore ≈ 0.867, got %.4f", tel.OverallScore)
	}
}

func TestComputeTelemetry_WithFailAndSkip(t *testing.T) {
	results := []core.EvaluationResult{
		{Passed: true, LatencyMS: 5, Score: core.Score{Normalized: 1.0, Weight: 1.0}},
		{Passed: false, LatencyMS: 2, Score: core.Score{Normalized: 0.0, Weight: 1.0}},
		{
			Passed:    false,
			LatencyMS: 0,
			Score:     core.Score{Normalized: 0.0, Weight: 1.0},
			Details:   map[string]any{"skipped_reason": "upstream dependency did not pass"},
		},
	}
	tel := storage.ComputeTelemetry(results, 0.005)

	if tel.Passed {
		t.Error("expected Passed==false when a node failed")
	}
	if tel.PassedCount != 1 {
		t.Errorf("expected PassedCount=1, got %d", tel.PassedCount)
	}
	if tel.FailedCount != 1 {
		t.Errorf("expected FailedCount=1, got %d", tel.FailedCount)
	}
	if tel.SkippedCount != 1 {
		t.Errorf("expected SkippedCount=1, got %d", tel.SkippedCount)
	}
	if tel.CostUSD != 0.005 {
		t.Errorf("expected CostUSD=0.005, got %f", tel.CostUSD)
	}
}

func TestComputeTelemetry_Empty(t *testing.T) {
	tel := storage.ComputeTelemetry(nil, 0)
	if tel.Passed {
		t.Error("empty result set should not be considered passed")
	}
	if tel.OverallScore != 0 {
		t.Errorf("expected OverallScore=0 for empty results, got %f", tel.OverallScore)
	}
}

// ---------------------------------------------------------------------------
// ToResultRecords
// ---------------------------------------------------------------------------

func TestToResultRecords_ErrConvertedToString(t *testing.T) {
	import_err := &testError{"boom"}
	results := []core.EvaluationResult{
		{EvaluatorID: "x", Passed: false, Err: import_err},
	}
	recs := storage.ToResultRecords(results)
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0].ErrMessage != "boom" {
		t.Errorf("expected ErrMessage=%q, got %q", "boom", recs[0].ErrMessage)
	}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

// ---------------------------------------------------------------------------
// LocalStorage — Save + LoadLatestBaseline round-trip
// ---------------------------------------------------------------------------

func makeRecord(datasetID string, passed bool, ts time.Time) storage.RunRecord {
	return storage.RunRecord{
		RunID:         datasetID + "-test",
		Timestamp:     ts,
		DatasetID:     datasetID,
		ConfigVersion: "1.0",
		Synced:        false,
		Telemetry: storage.Telemetry{
			AvgLatencyMS: 5,
			OverallScore: 1.0,
			Passed:       passed,
			EvalCount:    2,
			PassedCount:  2,
		},
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewLocalStorage(dir)

	rec := makeRecord("ds-alpha", true, time.Now())
	if err := s.Save(rec); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := s.LoadLatestBaseline("ds-alpha")
	if err != nil {
		t.Fatalf("LoadLatestBaseline failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected a baseline record, got nil")
	}
	if got.DatasetID != "ds-alpha" {
		t.Errorf("expected DatasetID=%q, got %q", "ds-alpha", got.DatasetID)
	}
	if !got.Telemetry.Passed {
		t.Error("expected baseline Telemetry.Passed==true")
	}
}

func TestLoadLatestBaseline_NoHistory(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewLocalStorage(dir)

	got, err := s.LoadLatestBaseline("nonexistent-dataset")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil baseline for empty history, got %+v", got)
	}
}

func TestLoadLatestBaseline_ReturnsLatestPassed(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewLocalStorage(dir)

	base := time.Now().UTC()

	// older passed run
	older := makeRecord("ds-beta", true, base.Add(-2*time.Hour))
	older.Telemetry.OverallScore = 0.8
	if err := s.Save(older); err != nil {
		t.Fatal(err)
	}

	// Wait a nanosecond to ensure distinct unixNano timestamps.
	// (In tests time.Now() can be identical across rapid calls.)
	time.Sleep(time.Millisecond)

	// newer passed run
	newer := makeRecord("ds-beta", true, base.Add(-1*time.Hour))
	newer.Telemetry.OverallScore = 0.95
	if err := s.Save(newer); err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadLatestBaseline("ds-beta")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected a baseline, got nil")
	}
	// Should return the newer one.
	if got.Telemetry.OverallScore != 0.95 {
		t.Errorf("expected newer baseline (score=0.95), got score=%.2f", got.Telemetry.OverallScore)
	}
}

func TestLoadLatestBaseline_SkipsFailedRuns(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewLocalStorage(dir)

	// Save a failed run.
	failed := makeRecord("ds-gamma", false, time.Now())
	failed.Telemetry.Passed = false
	if err := s.Save(failed); err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadLatestBaseline("ds-gamma")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil — failed runs must not be used as baseline")
	}
}

func TestLoadLatestBaseline_IsolatedByDatasetID(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewLocalStorage(dir)

	if err := s.Save(makeRecord("ds-x", true, time.Now())); err != nil {
		t.Fatal(err)
	}

	// Query for a different dataset ID — must return nil.
	got, err := s.LoadLatestBaseline("ds-y")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil for ds-y, got record for %q", got.DatasetID)
	}
}

func TestSave_CreatesDateShardedDirectories(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewLocalStorage(dir)

	ts := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	rec := makeRecord("sharding-test", true, ts)
	rec.Timestamp = ts

	if err := s.Save(rec); err != nil {
		t.Fatal(err)
	}

	expectedDir := filepath.Join(dir, "history", "2026", "07")
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("expected directory %q to exist", expectedDir)
	}
}

// ---------------------------------------------------------------------------
// LoadUnsynced + MarkSynced (Phase 3 sync support)
// ---------------------------------------------------------------------------

func TestLoadUnsynced_ReturnsUnsyncedOnly(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewLocalStorage(dir)

	// Save one unsynced and one already-synced record.
	unsynced := makeRecord("ds-sync-a", true, time.Now())
	unsynced.Synced = false
	if err := s.Save(unsynced); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)

	alreadySynced := makeRecord("ds-sync-a", true, time.Now())
	alreadySynced.Synced = true
	if err := s.Save(alreadySynced); err != nil {
		t.Fatal(err)
	}

	runs, err := s.LoadUnsynced()
	if err != nil {
		t.Fatalf("LoadUnsynced failed: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 unsynced run, got %d", len(runs))
	}
	if runs[0].Record.Synced {
		t.Error("loaded run should have Synced==false")
	}
}

func TestLoadUnsynced_EmptyHistory(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewLocalStorage(dir)

	runs, err := s.LoadUnsynced()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("expected 0 unsynced runs for empty history, got %d", len(runs))
	}
}

func TestMarkSynced_UpdatesFilesOnDisk(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewLocalStorage(dir)

	// Save two unsynced records.
	rec1 := makeRecord("ds-mark-a", true, time.Now())
	if err := s.Save(rec1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	rec2 := makeRecord("ds-mark-a", true, time.Now())
	if err := s.Save(rec2); err != nil {
		t.Fatal(err)
	}

	// Load them, mark as synced, then reload to verify.
	runs, err := s.LoadUnsynced()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 unsynced runs, got %d", len(runs))
	}

	if err := s.MarkSynced(runs); err != nil {
		t.Fatalf("MarkSynced failed: %v", err)
	}

	// After marking, LoadUnsynced should return nothing.
	remaining, err := s.LoadUnsynced()
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 unsynced runs after MarkSynced, got %d", len(remaining))
	}
}

func TestMarkSynced_FileContainsSyncedTrue(t *testing.T) {
	dir := t.TempDir()
	s := storage.NewLocalStorage(dir)

	rec := makeRecord("ds-synced-content", true, time.Now())
	if err := s.Save(rec); err != nil {
		t.Fatal(err)
	}

	runs, _ := s.LoadUnsynced()
	if len(runs) == 0 {
		t.Fatal("expected at least 1 unsynced run")
	}

	if err := s.MarkSynced(runs); err != nil {
		t.Fatal(err)
	}

	// Read the raw file and verify the JSON has synced=true.
	data, err := os.ReadFile(runs[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(runs[0].Path) {
		t.Error("UnsyncedRun.Path should be an absolute path")
	}
	// Verify JSON contains "synced": true
	if !containsBytes(data, []byte(`"synced": true`)) {
		t.Errorf("expected synced=true in file contents, got: %s", string(data))
	}
}

func containsBytes(haystack, needle []byte) bool {
	return bytes.Contains(haystack, needle)
}

