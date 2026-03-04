package runner

import (
	"context"
	"time"
)

// Task describes a unit of work to be executed by a Runner.
type Task struct {
	Type    string
	Input   map[string]any
	Timeout time.Duration
	Fn      func(ctx context.Context, input map[string]any) (map[string]any, error)
}

// Result holds the outcome of executing a Task.
type Result struct {
	Output   map[string]any
	Duration time.Duration
	Error    error
}

// Runner executes tasks. The in-process implementation calls Fn directly;
// future implementations may dispatch to GitHub Actions, Cloud Run Jobs, etc.
type Runner interface {
	Execute(ctx context.Context, task Task) (Result, error)
}
