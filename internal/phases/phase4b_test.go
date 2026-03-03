package phases

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestPhase4b(t *testing.T) {
	tests := []struct {
		name       string
		llm        *mockProvider
		wantNil    bool
		wantLabel  string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name: "misclassification detected",
			llm: &mockProvider{
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `{"classification": "question", "confidence": 90, "reason": "The issue is asking how to configure something."}`, nil
				},
			},
			wantLabel: "question",
		},
		{
			name: "classification agrees with current label",
			llm: &mockProvider{
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `{"classification": "bug", "confidence": 95, "reason": "This is clearly a bug report."}`, nil
				},
			},
			wantNil: true,
		},
		{
			name: "confidence below 80 returns nil",
			llm: &mockProvider{
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `{"classification": "enhancement", "confidence": 70, "reason": "Might be an enhancement."}`, nil
				},
			},
			wantNil: true,
		},
		{
			name: "invalid classification returns nil",
			llm: &mockProvider{
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `{"classification": "invalid_type", "confidence": 95, "reason": "Unknown type."}`, nil
				},
			},
			wantNil: true,
		},
		{
			name: "empty reason returns nil",
			llm: &mockProvider{
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `{"classification": "enhancement", "confidence": 90, "reason": ""}`, nil
				},
			},
			wantNil: true,
		},
		{
			name: "malformed LLM JSON object returns error",
			llm: &mockProvider{
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `{"classification": not_valid}`, nil
				},
			},
			wantErr:    true,
			wantErrMsg: "parse classification",
		},
		{
			name: "LLM response with no JSON braces returns nil",
			llm: &mockProvider{
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return "no json here", nil
				},
			},
			wantNil: true, // extractJSONObject returns "{}" which unmarshals to zero values; empty reason triggers nil
		},
		{
			name: "LLM generation error propagates",
			llm: &mockProvider{
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return "", fmt.Errorf("service unavailable")
				},
			},
			wantErr:    true,
			wantErrMsg: "generate classification",
		},
		{
			name: "confidence exactly 80 triggers misclassification",
			llm: &mockProvider{
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					return `{"classification": "enhancement", "confidence": 80, "reason": "This looks like an enhancement request."}`, nil
				},
			},
			wantLabel: "enhancement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Phase4b(context.Background(), tt.llm, testLogger(), "Test Title", "Test Body", "bug")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil result, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil result, got nil")
			}
			if result.SuggestedLabel != tt.wantLabel {
				t.Errorf("got label %q, want %q", result.SuggestedLabel, tt.wantLabel)
			}
		})
	}
}
