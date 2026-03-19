package main

import "testing"

func TestParseWeeksParam(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty defaults to 12", "", 12},
		{"valid", "8", 8},
		{"too high clamped", "100", 52},
		{"non-numeric defaults", "abc", 12},
		{"zero defaults", "0", 12},
		{"negative defaults", "-5", 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseWeeksParam(tt.input); got != tt.want {
				t.Errorf("parseWeeksParam(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
