// Package reporter contains concrete implementations of core.Reporter.
package reporter

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/pranavkakde/caliper/internal/config"
	"github.com/pranavkakde/caliper/pkg/core"
)

// ---------------------------------------------------------------------------
// ConsoleReporter
// ---------------------------------------------------------------------------

const (
	colWidth = 64 // inner width of the ASCII box (excludes border chars)
)

// ConsoleReporter prints a formatted ASCII summary of the DAG evaluation
// results to an io.Writer (defaulting to os.Stdout).
//
// Output format:
//
//	┌────────────────────────────────────────────────────────────────┐
//	│  CALIPER EVALUATION REPORT                                      │
//	╞════════════════════════════════════════════════════════════════╡
//	│  Evaluator                    Status   Score   Wt    Latency   │
//	├────────────────────────────────────────────────────────────────┤
//	│  ✓ regex-check                PASS     1.00   1.0     2ms      │
//	│  ✗ length-check               FAIL     0.00   0.5     1ms      │
//	│  ○ downstream                 SKIP     —      1.0     —        │
//	├────────────────────────────────────────────────────────────────┤
//	│  Result: FAIL  │  1 passed · 1 failed · 1 skipped             │
//	│  Weighted Score: 0.50                                          │
//	└────────────────────────────────────────────────────────────────┘
type ConsoleReporter struct {
	w io.Writer
}

// NewConsole returns a ConsoleReporter writing to os.Stdout.
func NewConsole() *ConsoleReporter {
	return &ConsoleReporter{w: os.Stdout}
}

// NewConsoleWriter returns a ConsoleReporter writing to w.
// Useful for capturing output in tests.
func NewConsoleWriter(w io.Writer) *ConsoleReporter {
	return &ConsoleReporter{w: w}
}

// Report implements core.Reporter. It prints a full ASCII summary for the
// provided results slice. Returns nil; console I/O errors are swallowed
// because they cannot affect the evaluation outcome.
func (c *ConsoleReporter) Report(_ context.Context, results []core.EvaluationResult) error {
	thin := strings.Repeat("─", colWidth)
	thick := strings.Repeat("═", colWidth)
	blank := strings.Repeat(" ", colWidth)

	row := func(content string) {
		// Left-pad content to colWidth, then add borders.
		padded := fmt.Sprintf("%-*s", colWidth, content)
		fmt.Fprintf(c.w, "│%s│\n", padded)
	}

	// ── Header ──────────────────────────────────────────────────────
	fmt.Fprintf(c.w, "┌%s┐\n", thin)
	row("  CALIPER EVALUATION REPORT")
	fmt.Fprintf(c.w, "╞%s╡\n", thick)
	row(fmt.Sprintf("  %-28s %-7s %-6s %-5s %s",
		"Evaluator", "Status", "Score", "Wt", "Latency"))
	fmt.Fprintf(c.w, "├%s┤\n", thin)

	// ── Per-node rows ────────────────────────────────────────────────
	var (
		passed, failed, skipped int
		weightedSum, totalWeight float64
	)

	for _, r := range results {
		isSkipped := !r.Passed && r.Details["skipped_reason"] != nil

		var icon, statusLabel, scoreStr, latStr string
		switch {
		case r.Passed:
			icon = "✓"
			statusLabel = "PASS"
			passed++
		case isSkipped:
			icon = "○"
			statusLabel = "SKIP"
			skipped++
		default:
			icon = "✗"
			statusLabel = "FAIL"
			failed++
		}

		if isSkipped {
			scoreStr = "—"
			latStr = "—"
		} else {
			scoreStr = fmt.Sprintf("%.2f", r.Score.Normalized)
			latStr = fmt.Sprintf("%dms", r.LatencyMS)
		}

		wt := r.Score.Weight
		if wt == 0 {
			wt = 1.0
		}
		wtStr := fmt.Sprintf("%.1f", wt)

		if r.Passed {
			weightedSum += r.Score.Normalized * wt
		}
		totalWeight += wt

		line := fmt.Sprintf("  %s %-28s %-7s %-6s %-5s %s",
			icon,
			truncate(r.EvaluatorID, 28),
			statusLabel,
			scoreStr,
			wtStr,
			latStr,
		)
		row(line)

		// If the node has an error, print it on the next line indented.
		if r.Err != nil {
			row(fmt.Sprintf("    └─ error: %s", truncate(r.Err.Error(), colWidth-14)))
		}
	}

	// ── Footer ───────────────────────────────────────────────────────
	fmt.Fprintf(c.w, "├%s┤\n", thin)

	overallStatus := "PASS"
	if failed > 0 {
		overallStatus = "FAIL"
	}

	countLine := fmt.Sprintf("  %d passed · %d failed · %d skipped",
		passed, failed, skipped)
	row(fmt.Sprintf("  Result: %-4s  │%s", overallStatus, countLine))

	weightedScore := 0.0
	if totalWeight > 0 {
		weightedScore = weightedSum / totalWeight
	}
	// Round to 2 decimal places.
	weightedScore = math.Round(weightedScore*100) / 100
	row(fmt.Sprintf("  Weighted Score: %.2f", weightedScore))
	row(blank)
	fmt.Fprintf(c.w, "└%s┘\n", thin)

	return nil
}

// truncate clips s to max runes, appending "…" if clipped.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// Build constructs a slice of core.Reporter from the ReporterConfig list in
// the YAML. If cfg is empty, a default ConsoleReporter is returned.
func Build(cfgs []config.ReporterConfig) ([]core.Reporter, error) {
	if len(cfgs) == 0 {
		return []core.Reporter{NewConsole()}, nil
	}
	reporters := make([]core.Reporter, 0, len(cfgs))
	for _, cfg := range cfgs {
		r, err := buildOne(cfg)
		if err != nil {
			return nil, err
		}
		reporters = append(reporters, r)
	}
	return reporters, nil
}

func buildOne(cfg config.ReporterConfig) (core.Reporter, error) {
	switch cfg.Type {
	case "console":
		return NewConsole(), nil
	default:
		return nil, fmt.Errorf(
			"reporter type %q is not supported in Phase 1 (supported: \"console\")",
			cfg.Type,
		)
	}
}
