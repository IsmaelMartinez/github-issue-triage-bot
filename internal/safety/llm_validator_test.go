package safety

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// mockProvider implements llm.Provider for testing.
type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) GenerateJSON(_ context.Context, _ string, _ float64, _ int) (string, error) {
	return m.response, m.err
}

func (m *mockProvider) GenerateJSONWithSystem(_ context.Context, _, _ string, _ float64, _ int) (string, error) {
	return m.response, m.err
}

func (m *mockProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}

func TestLLMValidator(t *testing.T) {
	tests := []struct {
		name       string
		response   string
		err        error
		wantPassed bool
		wantReason string
	}{
		{
			name:       "pass: safe output",
			response:   `{"passed": true, "reason": "appropriate", "confidence": 0.95}`,
			wantPassed: true,
			wantReason: "appropriate",
		},
		{
			name:       "fail: reflected injection",
			response:   `{"passed": false, "reason": "reflected injection", "confidence": 0.85}`,
			wantPassed: false,
			wantReason: "reflected injection",
		},
		{
			name:       "fail-safe: malformed JSON",
			response:   `"not json"`,
			wantPassed: false,
			wantReason: "failed to parse",
		},
		{
			name:       "fail-safe: LLM error",
			err:        errors.New("API unavailable"),
			wantPassed: false,
			wantReason: "LLM validation call failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewLLMValidator(&mockProvider{response: tt.response, err: tt.err})
			result := v.ValidateWithContext(context.Background(), "some output", "some issue")

			if result.Passed != tt.wantPassed {
				t.Fatalf("Passed = %v, want %v (reason: %s)", result.Passed, tt.wantPassed, result.Reason)
			}
			if !strings.Contains(result.Reason, tt.wantReason) {
				t.Fatalf("Reason = %q, want it to contain %q", result.Reason, tt.wantReason)
			}
		})
	}
}
