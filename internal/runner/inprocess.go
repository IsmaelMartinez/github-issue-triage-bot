package runner

import (
	"context"
	"time"
)

// InProcessRunner executes tasks by calling Fn directly in the current process.
type InProcessRunner struct{}

// NewInProcessRunner returns a new InProcessRunner.
func NewInProcessRunner() *InProcessRunner {
	return &InProcessRunner{}
}

// Execute runs the task's Fn with a child context bounded by the task's Timeout.
// It measures wall-clock duration and returns the result. On error, the error
// appears both in Result.Error and as the function's error return value.
func (r *InProcessRunner) Execute(ctx context.Context, task Task) (Result, error) {
	if task.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, task.Timeout)
		defer cancel()
	}

	start := time.Now()
	output, err := task.Fn(ctx, task.Input)
	duration := time.Since(start)

	res := Result{
		Output:   output,
		Duration: duration,
		Error:    err,
	}
	return res, err
}
