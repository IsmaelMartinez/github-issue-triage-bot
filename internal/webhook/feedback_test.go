package webhook

import (
	"strings"
	"testing"
)

func TestComputeFilledSections(t *testing.T) {
	// A complete template body with all sections filled in
	allFilled := strings.Join([]string{
		"### Reproduction steps",
		"",
		"1. Open the app",
		"2. Click login",
		"3. See error",
		"",
		"### Expected Behavior",
		"",
		"Should work correctly",
		"",
		"### Debug",
		"",
		"```",
		"[ERROR] Connection refused at line 42",
		"```",
		"",
		"### Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?",
		"",
		"Yes",
	}, "\n")

	// Template body with all defaults (nothing filled)
	allDefault := strings.Join([]string{
		"### Reproduction steps",
		"",
		"1. ...",
		"2. ...",
		"3. ...",
		"",
		"### Expected Behavior",
		"",
		"_No response_",
		"",
		"### Debug",
		"",
		"```bash",
		"ELECTRON_ENABLE_LOGGING=true teams-for-linux --logConfig='{\"transports\":{\"console\":{\"level\":\"debug\"}}}'",
		"```",
		"",
		"### Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?",
		"",
		"No",
	}, "\n")

	// Template with only repro steps default, rest filled
	reproDefault := strings.Join([]string{
		"### Reproduction steps",
		"",
		"1. ...",
		"2. ...",
		"3. ...",
		"",
		"### Expected Behavior",
		"",
		"Should display the login page",
		"",
		"### Debug",
		"",
		"```",
		"[ERROR] Segfault at 0xdeadbeef",
		"```",
		"",
		"### Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?",
		"",
		"No",
	}, "\n")

	// Same as reproDefault but with repro steps filled
	reproFilled := strings.Join([]string{
		"### Reproduction steps",
		"",
		"1. Open the app",
		"2. Click on Settings",
		"3. Toggle dark mode",
		"",
		"### Expected Behavior",
		"",
		"Should display the login page",
		"",
		"### Debug",
		"",
		"```",
		"[ERROR] Segfault at 0xdeadbeef",
		"```",
		"",
		"### Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?",
		"",
		"No",
	}, "\n")

	tests := []struct {
		name     string
		oldBody  string
		newBody  string
		wantFill []string
	}{
		{
			name:     "user fills all missing sections",
			oldBody:  allDefault,
			newBody:  allFilled,
			wantFill: []string{"Reproduction steps", "Debug console output", "Expected behavior"},
		},
		{
			name:     "user fills one section",
			oldBody:  reproDefault,
			newBody:  reproFilled,
			wantFill: []string{"Reproduction steps"},
		},
		{
			name:     "user edits but doesn't fill missing sections",
			oldBody:  reproDefault,
			newBody:  reproDefault, // same default content
			wantFill: nil,
		},
		{
			name:     "free-form body to templated body",
			oldBody:  "App crashes on login",
			newBody:  allFilled,
			wantFill: []string{"Reproduction steps", "Debug console output", "Expected behavior", "PWA reproducibility"},
		},
		{
			name:     "no template in either body",
			oldBody:  "Bug report text v1",
			newBody:  "Bug report text v2",
			wantFill: nil,
		},
		{
			name:     "user removes content (regression)",
			oldBody:  reproFilled,
			newBody:  reproDefault,
			wantFill: nil,
		},
		{
			name:     "empty to empty",
			oldBody:  "",
			newBody:  "",
			wantFill: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeFilledSections(tt.oldBody, tt.newBody)
			if len(got) != len(tt.wantFill) {
				t.Fatalf("computeFilledSections() returned %v (len=%d), want %v (len=%d)", got, len(got), tt.wantFill, len(tt.wantFill))
			}
			for i, label := range got {
				if label != tt.wantFill[i] {
					t.Errorf("computeFilledSections()[%d] = %q, want %q", i, label, tt.wantFill[i])
				}
			}
		})
	}
}

func TestBotMentionDetection(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "comment contains bot mention",
			body: "Thanks @ismael-triage-bot, the debug logs helped!",
			want: true,
		},
		{
			name: "comment does not contain bot mention",
			body: "I tried the suggested fix but it didn't work",
			want: false,
		},
		{
			name: "mention in middle of word",
			body: "not@ismael-triage-bot",
			want: true,
		},
		{
			name: "different case",
			body: "@Ismael-Triage-Bot thanks",
			want: false,
		},
		{
			name: "empty body",
			body: "",
			want: false,
		},
		{
			name: "mention at start",
			body: "@ismael-triage-bot this was helpful",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Contains(tt.body, botMentionHandle)
			if got != tt.want {
				t.Errorf("strings.Contains(%q, botMentionHandle) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}
