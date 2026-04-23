package config

import "encoding/json"

// ProjectMeta holds per-repo metadata used to parameterize LLM prompts and
// bot output. Defaults match the teams-for-linux deployment for backward
// compatibility; other repos override these in their butler.json.
type ProjectMeta struct {
	Name         string `json:"name"`          // human-readable project name (e.g. "Teams for Linux")
	Description  string `json:"description"`   // architecture description for research prompts
	DocsURL      string `json:"docs_url"`      // base URL for project documentation site
	DebugCommand string `json:"debug_command"` // shell command users run to capture debug logs
}

type ButlerConfig struct {
	Enabled          *bool               `json:"enabled"` // nil treated as true (kill switch)
	Project          ProjectMeta         `json:"project"`
	Capabilities     Capabilities        `json:"capabilities"`
	DocPaths         []string            `json:"doc_paths"`
	Upstream         []UpstreamDep       `json:"upstream"`
	Synthesis        SynthesisConfig     `json:"synthesis"`
	ShadowRepo       string              `json:"shadow_repo"`
	Thresholds       map[string]float64  `json:"thresholds"`
	MaxDailyLLMCalls int                 `json:"max_daily_llm_calls"`
	ResearchBrief    ResearchBriefConfig `json:"research_brief"`
}

// IsEnabled returns false only when Enabled is explicitly set to false.
func (c ButlerConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

type Capabilities struct {
	Triage         bool `json:"triage"`
	Research       bool `json:"research"`
	Synthesis      bool `json:"synthesis"`
	AutoIngest     bool `json:"auto_ingest"`
	CodeNavigation bool `json:"code_navigation"`
}

type UpstreamDep struct {
	Repo    string `json:"repo"`
	DocType string `json:"doc_type"`
	Track   string `json:"track"`
}

type SynthesisConfig struct {
	Frequency string `json:"frequency"`
	Day       string `json:"day"`
}

type ResearchBriefConfig struct {
	Enabled  bool   `json:"enabled"`
	HatsPath string `json:"hats_path"`
}

func DefaultConfig() ButlerConfig {
	return ButlerConfig{
		Project: ProjectMeta{
			Name: "Teams for Linux",
			Description: "an Electron desktop wrapper around the Microsoft Teams web app.\n\n" +
				"Project architecture:\n" +
				"- Desktop client: Electron + Node.js wrapping the Teams web app in a BrowserWindow\n" +
				"- Custom CSS injection for theming (user-provided CSS files loaded at startup)\n" +
				"- Configuration via config.json and CLI flags (Electron flags, proxy, notifications, etc.)\n" +
				"- System tray integration, notifications, and keyboard shortcuts via Electron APIs\n" +
				"- Preload scripts for bridging web app and native features\n" +
				"- The app cannot modify the Teams web UI itself — features must work through Electron APIs, CSS injection, or configuration",
			DocsURL:      "https://ismaelmartinez.github.io/teams-for-linux",
			DebugCommand: `ELECTRON_ENABLE_LOGGING=true teams-for-linux --logConfig='{"transports":{"console":{"level":"debug"}}}'`,
		},
		Capabilities:     Capabilities{Triage: true, Research: true},
		DocPaths:         []string{"docs/**", "*.md"},
		Synthesis:        SynthesisConfig{Frequency: "weekly", Day: "monday"},
		MaxDailyLLMCalls: 50,
		ResearchBrief: ResearchBriefConfig{
			Enabled:  false,
			HatsPath: ".github/hats.md",
		},
		Thresholds: map[string]float64{
			"troubleshooting":  0.70,
			"adr":              0.55,
			"roadmap":          0.55,
			"research":         0.55,
			"configuration":    0.50,
			"upstream_release": 0.45,
			"upstream_issue":   0.45,
		},
	}
}

func Parse(data []byte) (ButlerConfig, error) {
	cfg := DefaultConfig()
	if len(data) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ButlerConfig{}, err
	}
	if cfg.MaxDailyLLMCalls <= 0 {
		cfg.MaxDailyLLMCalls = 50
	}
	return cfg, nil
}
