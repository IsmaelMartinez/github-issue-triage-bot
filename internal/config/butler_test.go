package config

import "testing"

func TestParseButlerConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    ButlerConfig
		wantErr bool
	}{
		{
			name:  "full config",
			input: `{"capabilities":{"triage":true,"synthesis":true,"auto_ingest":true},"doc_paths":["docs/**"],"shadow_repo":"owner/shadow","max_daily_llm_calls":50}`,
			want: ButlerConfig{
				Capabilities:     Capabilities{Triage: true, Synthesis: true, AutoIngest: true},
				DocPaths:         []string{"docs/**"},
				ShadowRepo:       "owner/shadow",
				MaxDailyLLMCalls: 50,
			},
		},
		{
			name:  "empty input returns defaults",
			input: "",
			want:  DefaultConfig(),
		},
		{
			name:    "invalid JSON",
			input:   `{"bad":"json"`,
			wantErr: true,
		},
		{
			name:  "missing capabilities uses defaults",
			input: `{"shadow_repo":"owner/shadow"}`,
			want: func() ButlerConfig {
				c := DefaultConfig()
				c.ShadowRepo = "owner/shadow"
				return c
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.ShadowRepo != tt.want.ShadowRepo {
				t.Errorf("ShadowRepo = %q, want %q", got.ShadowRepo, tt.want.ShadowRepo)
			}
			if got.Capabilities.Synthesis != tt.want.Capabilities.Synthesis {
				t.Errorf("Synthesis = %v, want %v", got.Capabilities.Synthesis, tt.want.Capabilities.Synthesis)
			}
			if got.MaxDailyLLMCalls != tt.want.MaxDailyLLMCalls {
				t.Errorf("MaxDailyLLMCalls = %d, want %d", got.MaxDailyLLMCalls, tt.want.MaxDailyLLMCalls)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if !c.Capabilities.Triage {
		t.Error("default should enable triage")
	}
	if c.Capabilities.Synthesis {
		t.Error("default should disable synthesis")
	}
	if c.MaxDailyLLMCalls != 50 {
		t.Errorf("default MaxDailyLLMCalls = %d, want 50", c.MaxDailyLLMCalls)
	}
}

func TestIsEnabled(t *testing.T) {
	// Default (nil Enabled) should be enabled
	c := DefaultConfig()
	if !c.IsEnabled() {
		t.Error("default config should be enabled")
	}

	// Explicit true
	trueVal := true
	c.Enabled = &trueVal
	if !c.IsEnabled() {
		t.Error("explicit true should be enabled")
	}

	// Explicit false (kill switch)
	falseVal := false
	c.Enabled = &falseVal
	if c.IsEnabled() {
		t.Error("explicit false should be disabled")
	}

	// Parsed from JSON with enabled: false
	cfg, err := Parse([]byte(`{"enabled": false}`))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cfg.IsEnabled() {
		t.Error("parsed enabled:false should be disabled")
	}
}

func TestCodeNavigationDefault(t *testing.T) {
	c := DefaultConfig()
	if c.Capabilities.CodeNavigation {
		t.Error("code navigation should be disabled by default")
	}
	cfg, _ := Parse([]byte(`{"capabilities":{"code_navigation":true}}`))
	if !cfg.Capabilities.CodeNavigation {
		t.Error("code navigation should be enabled when set")
	}
}

func TestProjectMetaDefaults(t *testing.T) {
	c := DefaultConfig()
	if c.Project.Name != "Teams for Linux" {
		t.Errorf("default project name = %q, want %q", c.Project.Name, "Teams for Linux")
	}
	if c.Project.DocsURL == "" {
		t.Error("default DocsURL should not be empty")
	}
	if c.Project.DebugCommand == "" {
		t.Error("default DebugCommand should not be empty")
	}
	if c.Project.Description == "" {
		t.Error("default Description should not be empty")
	}
}

func TestProjectMetaParsing(t *testing.T) {
	input := `{"project":{"name":"My App","docs_url":"https://docs.myapp.io","debug_command":"myapp --debug","description":"a web dashboard"}}`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if cfg.Project.Name != "My App" {
		t.Errorf("Name = %q, want %q", cfg.Project.Name, "My App")
	}
	if cfg.Project.DocsURL != "https://docs.myapp.io" {
		t.Errorf("DocsURL = %q, want %q", cfg.Project.DocsURL, "https://docs.myapp.io")
	}
	if cfg.Project.DebugCommand != "myapp --debug" {
		t.Errorf("DebugCommand = %q, want %q", cfg.Project.DebugCommand, "myapp --debug")
	}
	if cfg.Project.Description != "a web dashboard" {
		t.Errorf("Description = %q, want %q", cfg.Project.Description, "a web dashboard")
	}
}

func TestProjectMetaEmptyOverride(t *testing.T) {
	// Explicitly setting empty strings should override defaults
	input := `{"project":{"name":"","docs_url":"","debug_command":""}}`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if cfg.Project.Name != "" {
		t.Errorf("Name should be empty when explicitly set, got %q", cfg.Project.Name)
	}
	if cfg.Project.DocsURL != "" {
		t.Errorf("DocsURL should be empty when explicitly set, got %q", cfg.Project.DocsURL)
	}
	if cfg.Project.DebugCommand != "" {
		t.Errorf("DebugCommand should be empty when explicitly set, got %q", cfg.Project.DebugCommand)
	}
}
