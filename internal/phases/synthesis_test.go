package phases

import (
	"context"
	"strings"
	"testing"
)

func TestShouldSynthesize(t *testing.T) {
	tests := []struct {
		name  string
		input SynthesisInput
		want  bool
	}{
		{
			name:  "empty input returns false",
			input: SynthesisInput{},
			want:  false,
		},
		{
			name: "phase1 with one missing item and no docs returns false",
			input: SynthesisInput{
				IsBug: true,
				Phase1: Phase1Result{
					MissingItems: []MissingItem{{Label: "Debug", Detail: "missing"}},
				},
			},
			want: false,
		},
		{
			name: "phase1 with two missing items returns true",
			input: SynthesisInput{
				IsBug: true,
				Phase1: Phase1Result{
					MissingItems: []MissingItem{
						{Label: "Debug", Detail: "missing"},
						{Label: "Steps", Detail: "missing"},
					},
				},
			},
			want: true,
		},
		{
			name: "phase2 only returns true",
			input: SynthesisInput{
				IsBug:  true,
				Phase2: []Suggestion{{Title: "Login fix", DocURL: "https://example.com", Reason: "related"}},
			},
			want: true,
		},
		{
			name: "phase4a only returns true",
			input: SynthesisInput{
				IsEnhancement: true,
				Phase4a:       []ContextMatch{{Topic: "SSO", DocURL: "https://example.com", Reason: "related"}},
			},
			want: true,
		},
		{
			name: "phase1 one item plus phase2 returns true",
			input: SynthesisInput{
				IsBug: true,
				Phase1: Phase1Result{
					MissingItems: []MissingItem{{Label: "Debug", Detail: "missing"}},
				},
				Phase2: []Suggestion{{Title: "Related", DocURL: "https://example.com", Reason: "similar"}},
			},
			want: true,
		},
		{
			name: "pwa note only returns true",
			input: SynthesisInput{
				IsBug: true,
				Phase1: Phase1Result{
					IsPwaReproducible: true,
				},
			},
			want: true,
		},
		{
			name: "pwa note suppressed for doc bugs",
			input: SynthesisInput{
				IsBug:    true,
				IsDocBug: true,
				Phase1: Phase1Result{
					IsPwaReproducible: true,
				},
			},
			want: false,
		},
		{
			name: "pwa note suppressed for non-bugs",
			input: SynthesisInput{
				IsEnhancement: true,
				Phase1: Phase1Result{
					IsPwaReproducible: true,
				},
			},
			want: false,
		},
		{
			name: "all phases have output returns true",
			input: SynthesisInput{
				IsBug: true,
				Phase1: Phase1Result{
					MissingItems:      []MissingItem{{Label: "Debug", Detail: "missing"}},
					IsPwaReproducible: true,
				},
				Phase2:  []Suggestion{{Title: "Doc", DocURL: "https://example.com", Reason: "match"}},
				Phase4a: []ContextMatch{{Topic: "Feature", DocURL: "https://example.com", Reason: "related"}},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldSynthesize(tt.input)
			if got != tt.want {
				t.Errorf("ShouldSynthesize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildSynthesisPrompt(t *testing.T) {
	tests := []struct {
		name         string
		input        SynthesisInput
		wantContains []string
		wantMissing  []string
	}{
		{
			name: "includes issue title and body",
			input: SynthesisInput{
				IssueTitle: "App crashes on startup",
				IssueBody:  "When I open the app it crashes immediately",
			},
			wantContains: []string{"App crashes on startup", "When I open the app"},
		},
		{
			name: "includes phase2 context",
			input: SynthesisInput{
				IssueTitle: "Bug",
				Phase2: []Suggestion{
					{Title: "Login Fix", DocURL: "https://example.com/login", Reason: "same symptoms"},
				},
			},
			wantContains: []string{"CONTEXT (only use these URLs)", "Login Fix", "https://example.com/login", "same symptoms"},
		},
		{
			name: "includes phase4a context",
			input: SynthesisInput{
				IssueTitle: "Feature request",
				Phase4a: []ContextMatch{
					{Topic: "SSO Support", DocURL: "https://example.com/sso", Reason: "planned work"},
				},
			},
			wantContains: []string{"CONTEXT (only use these URLs)", "SSO Support", "https://example.com/sso"},
		},
		{
			name: "includes missing information",
			input: SynthesisInput{
				IssueTitle: "Bug",
				IsBug:      true,
				Phase1: Phase1Result{
					MissingItems: []MissingItem{
						{Label: "Debug console output", Detail: "Please share debug logs"},
					},
				},
			},
			wantContains: []string{"MISSING INFORMATION", "Debug console output", "Please share debug logs"},
		},
		{
			name: "includes pwa note for bugs",
			input: SynthesisInput{
				IssueTitle: "Bug",
				IsBug:      true,
				Phase1: Phase1Result{
					IsPwaReproducible: true,
				},
			},
			wantContains: []string{"PWA NOTE", "feedbackportal.microsoft.com"},
		},
		{
			name: "omits pwa note for doc bugs",
			input: SynthesisInput{
				IssueTitle: "Doc bug",
				IsBug:      true,
				IsDocBug:   true,
				Phase1: Phase1Result{
					IsPwaReproducible: true,
				},
			},
			wantMissing: []string{"PWA NOTE"},
		},
		{
			name: "omits context section when no docs",
			input: SynthesisInput{
				IssueTitle: "Bug",
				IsBug:      true,
				Phase1: Phase1Result{
					MissingItems: []MissingItem{{Label: "Steps", Detail: "add steps"}},
				},
			},
			wantContains: []string{"MISSING INFORMATION"},
			wantMissing:  []string{"CONTEXT"},
		},
		{
			name: "truncates long body",
			input: SynthesisInput{
				IssueTitle: "Bug",
				IssueBody:  strings.Repeat("a", 3000),
			},
			wantContains: []string{"ISSUE:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSynthesisPrompt(tt.input)
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("prompt should contain %q, got:\n%s", want, got)
				}
			}
			for _, missing := range tt.wantMissing {
				if strings.Contains(got, missing) {
					t.Errorf("prompt should NOT contain %q, got:\n%s", missing, got)
				}
			}
		})
	}
}

func TestSynthesize(t *testing.T) {
	tests := []struct {
		name       string
		llmResp    string
		llmErr     error
		want       string
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:    "normal response",
			llmResp: `{"comment": "This looks like a login issue. Try clearing the cache."}`,
			want:    "This looks like a login issue. Try clearing the cache.",
		},
		{
			name:    "EMPTY response returns empty string",
			llmResp: `{"comment": "EMPTY"}`,
			want:    "",
		},
		{
			name:    "empty comment field returns empty string",
			llmResp: `{"comment": ""}`,
			want:    "",
		},
		{
			name:    "whitespace-only comment returns empty string",
			llmResp: `{"comment": "   "}`,
			want:    "",
		},
		{
			name:       "LLM error propagates",
			llmErr:     context.DeadlineExceeded,
			wantErr:    true,
			wantErrMsg: "synthesize",
		},
		{
			name:    "non-JSON response falls back to empty via ExtractJSONObject",
			llmResp: `not json at all`,
			want:    "",
		},
		{
			name:    "response with extra whitespace is trimmed",
			llmResp: `{"comment": "\n  Check the debug logs.  \n"}`,
			want:    "Check the debug logs.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockProvider{
				generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
					if tt.llmErr != nil {
						return "", tt.llmErr
					}
					return tt.llmResp, nil
				},
			}

			input := SynthesisInput{
				IssueTitle: "Test issue",
				IssueBody:  "Test body",
				IsBug:      true,
				Phase1: Phase1Result{
					MissingItems: []MissingItem{{Label: "Debug", Detail: "add logs"}},
				},
				Phase2: []Suggestion{{Title: "Doc", DocURL: "https://example.com", Reason: "related"}},
			}

			got, err := Synthesize(context.Background(), mock, input)
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
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSynthesizePromptParameters(t *testing.T) {
	var capturedSys, capturedUser string
	var capturedTemp float64
	var capturedMax int

	mock := &mockProvider{
		generateJSONWithSysFunc: func(ctx context.Context, sys, user string, temp float64, max int) (string, error) {
			capturedSys = sys
			capturedUser = user
			capturedTemp = temp
			capturedMax = max
			return `{"comment": "ok"}`, nil
		},
	}

	input := SynthesisInput{
		IssueTitle: "Test",
		IssueBody:  "Body",
		IsBug:      true,
		Phase2:     []Suggestion{{Title: "Doc", DocURL: "https://example.com", Reason: "match"}},
	}

	_, err := Synthesize(context.Background(), mock, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedTemp != 0.3 {
		t.Errorf("temperature = %v, want 0.3", capturedTemp)
	}
	if capturedMax != 2048 {
		t.Errorf("maxTokens = %d, want 2048", capturedMax)
	}
	if !strings.Contains(capturedSys, "triage assistant") {
		t.Error("system prompt should contain 'triage assistant'")
	}
	if !strings.Contains(capturedUser, "CONTEXT") {
		t.Error("user prompt should contain CONTEXT section when Phase2 has results")
	}
}
