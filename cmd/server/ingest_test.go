package main

import "testing"

func TestValidateIngestSecret(t *testing.T) {
	tests := []struct {
		name   string
		header string
		secret string
		want   bool
	}{
		{"valid", "Bearer mysecret", "mysecret", true},
		{"wrong secret", "Bearer wrong", "mysecret", false},
		{"missing bearer", "mysecret", "mysecret", false},
		{"empty header", "", "mysecret", false},
		{"empty secret disables auth", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateIngestAuth(tt.header, tt.secret); got != tt.want {
				t.Errorf("validateIngestAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}
