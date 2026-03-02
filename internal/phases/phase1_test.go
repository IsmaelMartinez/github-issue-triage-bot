package phases

import "testing"

func TestGetSection(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		header string
		want   string
	}{
		{
			name:   "extracts section content",
			body:   "### Describe the bug\n\nApp crashes on startup\n\n### Reproduction steps\n\n1. Open app\n2. Click login\n\n### Expected Behavior\n\nShould not crash",
			header: "Reproduction steps",
			want:   "1. Open app\n2. Click login",
		},
		{
			name:   "extracts last section",
			body:   "### First\n\nfirst content\n\n### Second\n\nsecond content",
			header: "Second",
			want:   "second content",
		},
		{
			name:   "returns empty for missing section",
			body:   "### Describe the bug\n\nsome bug",
			header: "Missing Section",
			want:   "",
		},
		{
			name:   "handles special characters in header",
			body:   "### Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?\n\nYes",
			header: "Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?",
			want:   "Yes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSection(tt.body, tt.header)
			if got != tt.want {
				t.Errorf("getSection() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsDefaultStepsTemplate(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty string", "", true},
		{"no response", "_No response_", true},
		{"only numbered markers", "1. ...\n2. ...\n3. ...", true},
		{"actual content", "1. Open the app\n2. Click login\n3. See error", false},
		{"whitespace only", "   \n   ", true},
		{"code fence only", "```\n```", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDefaultStepsTemplate(tt.content)
			if got != tt.want {
				t.Errorf("isDefaultStepsTemplate(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestIsDebugMissing(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty string", "", true},
		{"no response", "_No response_", true},
		{"only default template", "```bash\nELECTRON_ENABLE_LOGGING=true teams-for-linux --logConfig='{\"transports\":{\"console\":{\"level\":\"debug\"}}}'\n```", true},
		{"actual debug output", "```\n[12:00:01] Error: Connection refused\n[12:00:02] Retrying...\n```", false},
		{"whitespace only", "   ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDebugMissing(tt.content)
			if got != tt.want {
				t.Errorf("isDebugMissing(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestPhase1(t *testing.T) {
	tests := []struct {
		name             string
		body             string
		wantMissingCount int
		wantPWA          bool
	}{
		{
			name: "complete bug report with PWA",
			body: "### Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?\n\nYes\n\n### Describe the bug\n\nApp crashes\n\n### Reproduction steps\n\n1. Open app\n2. See crash\n\n### Expected Behavior\n\nShould work\n\n### Debug\n\n```\n[ERROR] Something failed\n```",
			wantMissingCount: 0,
			wantPWA:          true,
		},
		{
			name: "all missing with no PWA",
			body: "### Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?\n\nNo\n\n### Describe the bug\n\nSomething broken\n\n### Reproduction steps\n\n1. ...\n2. ...\n3. ...\n\n### Expected Behavior\n\n_No response_\n\n### Debug\n\n```bash\nELECTRON_ENABLE_LOGGING=true teams-for-linux --logConfig='{\"transports\":{\"console\":{\"level\":\"debug\"}}}'\n```",
			wantMissingCount: 3,
			wantPWA:          false,
		},
		{
			name:             "empty body",
			body:             "",
			wantMissingCount: 0,
			wantPWA:          false,
		},
		{
			name: "partial info",
			body: "### Can you reproduce this bug on the Microsoft Teams web app (https://teams.microsoft.com)?\n\nNo\n\n### Describe the bug\n\nCan't login\n\n### Reproduction steps\n\n1. Open app\n2. Enter credentials\n3. Click submit\n\n### Expected Behavior\n\n_No response_\n\n### Debug\n\n_No response_",
			wantMissingCount: 2,
			wantPWA:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Phase1(tt.body)
			if len(result.MissingItems) != tt.wantMissingCount {
				t.Errorf("Phase1() missing items = %d, want %d; items: %+v", len(result.MissingItems), tt.wantMissingCount, result.MissingItems)
			}
			if result.IsPwaReproducible != tt.wantPWA {
				t.Errorf("Phase1() PWA = %v, want %v", result.IsPwaReproducible, tt.wantPWA)
			}
		})
	}
}
