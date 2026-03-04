package runner

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInProcessRunner_Success(t *testing.T) {
	r := NewInProcessRunner()

	task := Task{
		Type:    "test",
		Input:   map[string]any{"key": "value"},
		Timeout: 5 * time.Second,
		Fn: func(ctx context.Context, input map[string]any) (map[string]any, error) {
			return map[string]any{"result": input["key"]}, nil
		},
	}

	result, err := r.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Error != nil {
		t.Fatalf("expected no result error, got %v", result.Error)
	}
	if result.Output["result"] != "value" {
		t.Fatalf("expected output result=value, got %v", result.Output["result"])
	}
	if result.Duration <= 0 {
		t.Fatalf("expected positive duration, got %v", result.Duration)
	}
}

func TestInProcessRunner_Timeout(t *testing.T) {
	r := NewInProcessRunner()

	task := Task{
		Type:    "slow",
		Timeout: 50 * time.Millisecond,
		Fn: func(ctx context.Context, input map[string]any) (map[string]any, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return map[string]any{"done": true}, nil
			}
		},
	}

	result, err := r.Execute(context.Background(), task)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if !errors.Is(result.Error, context.DeadlineExceeded) {
		t.Fatalf("expected result.Error to be DeadlineExceeded, got %v", result.Error)
	}
}
