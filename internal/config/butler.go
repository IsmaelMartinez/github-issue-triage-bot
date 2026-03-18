package config

import "encoding/json"

type ButlerConfig struct {
	Capabilities     Capabilities       `json:"capabilities"`
	DocPaths         []string           `json:"doc_paths"`
	Upstream         []UpstreamDep      `json:"upstream"`
	Synthesis        SynthesisConfig    `json:"synthesis"`
	ShadowRepo       string             `json:"shadow_repo"`
	Thresholds       map[string]float64 `json:"thresholds"`
	MaxDailyLLMCalls int                `json:"max_daily_llm_calls"`
}

type Capabilities struct {
	Triage     bool `json:"triage"`
	Research   bool `json:"research"`
	Synthesis  bool `json:"synthesis"`
	AutoIngest bool `json:"auto_ingest"`
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

func DefaultConfig() ButlerConfig {
	return ButlerConfig{
		Capabilities:     Capabilities{Triage: true, Research: true},
		DocPaths:         []string{"docs/**", "*.md"},
		Synthesis:        SynthesisConfig{Frequency: "weekly", Day: "monday"},
		MaxDailyLLMCalls: 50,
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
