package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifyWebhookSignature(t *testing.T) {
	tests := []struct {
		name      string
		payload   string
		signature string
		secret    string
		want      bool
	}{
		{
			name:      "empty signature",
			payload:   `{}`,
			signature: "",
			secret:    "test",
			want:      false,
		},
		{
			name:      "wrong prefix",
			payload:   `{}`,
			signature: "sha1=abc123",
			secret:    "test",
			want:      false,
		},
		{
			name:      "wrong signature",
			payload:   `{"action":"opened"}`,
			signature: "sha256=0000000000000000000000000000000000000000000000000000000000000000",
			secret:    "test-secret",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifyWebhookSignature([]byte(tt.payload), tt.signature, tt.secret)
			if got != tt.want {
				t.Errorf("VerifyWebhookSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVerifyWebhookSignature_Valid(t *testing.T) {
	payload := []byte(`{"test": true}`)
	secret := "my-secret"

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !VerifyWebhookSignature(payload, sig, secret) {
		t.Error("VerifyWebhookSignature() should return true for valid signature")
	}
}
