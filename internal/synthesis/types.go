package synthesis

import (
	"context"
	"time"
)

// Finding represents a single insight produced by a synthesizer.
type Finding struct {
	Type       string   // cluster, drift, upstream_signal, staleness
	Severity   string   // info, warning, action_needed
	Title      string
	Evidence   []string
	Suggestion string
}

// Synthesizer is the interface that each analysis module implements.
type Synthesizer interface {
	Name() string
	Analyze(ctx context.Context, repo string, window time.Duration) ([]Finding, error)
}
