package config

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		name     string
		config   ButlerConfig
		wantWarn int
	}{
		{
			name:     "default config is valid",
			config:   DefaultConfig(),
			wantWarn: 0,
		},
		{
			name: "synthesis without shadow repo",
			config: func() ButlerConfig {
				c := DefaultConfig()
				c.Capabilities.Synthesis = true
				return c
			}(),
			wantWarn: 1,
		},
		{
			name: "synthesis with shadow repo is valid",
			config: func() ButlerConfig {
				c := DefaultConfig()
				c.Capabilities.Synthesis = true
				c.ShadowRepo = "owner/shadow"
				return c
			}(),
			wantWarn: 0,
		},
		{
			name: "invalid frequency",
			config: func() ButlerConfig {
				c := DefaultConfig()
				c.Capabilities.Synthesis = true
				c.ShadowRepo = "owner/shadow"
				c.Synthesis.Frequency = "hourly"
				return c
			}(),
			wantWarn: 1,
		},
		{
			name: "invalid day",
			config: func() ButlerConfig {
				c := DefaultConfig()
				c.Capabilities.Synthesis = true
				c.ShadowRepo = "owner/shadow"
				c.Synthesis.Day = "notaday"
				return c
			}(),
			wantWarn: 1,
		},
		{
			name: "threshold out of range",
			config: func() ButlerConfig {
				c := DefaultConfig()
				c.Thresholds["test"] = 1.5
				return c
			}(),
			wantWarn: 1,
		},
		{
			name: "invalid glob pattern",
			config: func() ButlerConfig {
				c := DefaultConfig()
				c.DocPaths = []string{"docs/**", "[invalid"}
				return c
			}(),
			wantWarn: 1,
		},
		{
			name: "LLM calls above free tier",
			config: func() ButlerConfig {
				c := DefaultConfig()
				c.MaxDailyLLMCalls = 300
				return c
			}(),
			wantWarn: 1,
		},
		{
			name: "multiple warnings",
			config: func() ButlerConfig {
				c := DefaultConfig()
				c.Capabilities.Synthesis = true
				// no shadow repo
				c.Synthesis.Frequency = "biweekly"
				c.Thresholds["bad"] = -0.5
				return c
			}(),
			wantWarn: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := tt.config.Validate()
			if len(warnings) != tt.wantWarn {
				t.Errorf("Validate() returned %d warnings, want %d: %v", len(warnings), tt.wantWarn, warnings)
			}
		})
	}
}
