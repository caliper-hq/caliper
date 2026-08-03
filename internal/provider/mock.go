// Package provider contains concrete implementations of core.Provider and
// a factory function for constructing them from configuration.
package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/pranavkakde/caliper/internal/config"
	"github.com/pranavkakde/caliper/pkg/core"
)

// ---------------------------------------------------------------------------
// MockLLMProvider
// ---------------------------------------------------------------------------

// MockLLMProvider is a deterministic, zero-dependency provider that returns
// a pre-configured static string for every Complete call. It is the default
// provider for local development, CI pipelines, and unit tests.
//
// It honours context cancellation so the DAG engine's timeout semantics
// are exercised even without a real API.
type MockLLMProvider struct {
	response string
	delayMS  int // artificial latency in milliseconds (0 = instant)
}

// NewMock returns a MockLLMProvider that returns response for every prompt.
// If delayMS > 0, Complete sleeps for that many milliseconds before
// returning, simulating network latency.
func NewMock(response string, delayMS int) *MockLLMProvider {
	return &MockLLMProvider{response: response, delayMS: delayMS}
}

// Name implements core.Provider.
func (m *MockLLMProvider) Name() string { return "mock" }

// Complete implements core.Provider. It ignores the prompt entirely and
// returns the static response string configured at construction time.
func (m *MockLLMProvider) Complete(ctx context.Context, _ string) (string, error) {
	if m.delayMS > 0 {
		select {
		case <-time.After(time.Duration(m.delayMS) * time.Millisecond):
		case <-ctx.Done():
			return "", fmt.Errorf("mock provider: %w", ctx.Err())
		}
	}
	return m.response, nil
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// Build constructs a core.Provider from a ProviderConfig.
// Returns an error for unknown provider types; in Phase 1 only "mock" is
// supported.
func Build(cfg config.ProviderConfig) (core.Provider, error) {
	switch cfg.Type {
	case "mock":
		response := "This is a static mock LLM response."
		if r, ok := cfg.Params["response"].(string); ok && r != "" {
			response = r
		}
		delayMS := 0
		if d, ok := cfg.Params["delay_ms"].(int); ok {
			delayMS = d
		}
		return NewMock(response, delayMS), nil

	default:
		return nil, fmt.Errorf(
			"provider type %q is not supported in Phase 1 (only \"mock\" is available); "+
				"real provider adapters will be added in a future phase",
			cfg.Type,
		)
	}
}
