package main

import "testing"

func TestExtractElectronVersion(t *testing.T) {
	tests := []struct {
		topic string
		want  string
	}{
		{"v41.0.0 Release Notes", "41"},
		{"v39.8.2 Patch Release", "39"},
		{"Electron 42.1.0 breaking change: clipboard API", "42"},
		{"No version here", ""},
		{"Fix crash in BrowserWindow v40.3.1", "40"},
	}
	for _, tt := range tests {
		got := extractElectronVersion(tt.topic)
		if got != tt.want {
			t.Errorf("extractElectronVersion(%q) = %q, want %q", tt.topic, got, tt.want)
		}
	}
}
