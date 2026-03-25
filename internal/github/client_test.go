package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestGetTree(t *testing.T) {
	// newTestClient creates a Client with a fake appID/key pointing at a test server.
	// The ghinstallation transport fetches an installation token from
	// {baseURL}/app/installations/{id}/access_tokens, so the test server must
	// handle that endpoint as well as the actual tree endpoint.
	// testRSAKey is a test-only RSA private key (PKCS#1 PEM). Never used in production.
	const testRSAKey = `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA40vI/7k1gKWwDmFWQe/TSTyheQssz8EO4D85J16xoALln/H3
ChwHn2jwItFz8IkIlsJlyZeOXyYVPYOAYW1itmcLC5xhuUdAR0rUjzwXq7L/gVJJ
el29vCl/vgRvLeFeU8/tQ6/pXaQ/WcElt0E8vSbDnGbgHPpmuY7vs1FyVhwOPxGs
ygKa4wxGKfcxMlrae57q6TEHT2XJmrItsY88d2DDIxPVo8MPai7xIzN5ZG1knDKi
KsXPKUT8cp1ZUo1eT+iPRUK/G9WFsnOPyIw1ZI/QHHPQPvIy7nfPO3VfuqwOGwbV
Ag5ohD/4Kie9dsscMmW73DGJXozyUU7TViTyLwIDAQABAoIBAEyJxgbaopoN8RF+
lHHCpObJ/GPKsA3LaEt57rCDshN8Nj+cVoA4fRagWxCWcFCkjFhb4LO4DbCbndZn
dDEaiP18CFuiDsQ5qnr3R0luRlhCf8hX4bdLXqtAXCwryRZth/p4D2DWGSK3vr9m
C2HAnYfiSEdf2wLXDQVaDPxYpkQ5LstiF9Amrg8jqKXru3PdmKPbz8M+rCeOs2jS
l/rPqfcpchMYfEHn/Buf4k+7/elFQXcZQOEFGmcVl0pj0ugGSZaoUwJ1ptuN1rVu
qv8BncMDbDrB5FYAjbSSZng53Y6OJKsUeDGdZ2RUw1n7VXkTF373Ma2UDicYTnrG
ogyMf/0CgYEA+B257iOe5rb2qWdHmIJcEOvdepO7YsLHOyrixSgjrsZdIRnxHVmj
PoDlVoZ0PBEJRzCCe26kfQf3g/DRWDGGR0sdaFjXel+64C2IAd6MCcTt3G73N2/t
mPhhyAU2fJKFI7MgBFdJaa36CL8zGtFy88s/g9Z1z1AGliGbkJyrtO0CgYEA6oSz
PUf8nCfvT5DUV3hC5es6226z0j1wEZyvifnBVwKKr6F1LpKchL4obBUUIl6ZzFG5
ZZ9YJ/SJj7seQKRhfUOQQXMx0Ind8H6Hou//Jvth81Pcn2YZ0NukXlWFlmkZdmsR
H7LUnvPULOD8Zvgvz7TwNlTWpfUjX9VqCrvbXAsCgYEAsR2PP3bAFNQhClbGnhDY
pd+pj7nrtxlx3UPE85auujGyA1Igc6IsTQ74J6b9TG+g3ue7DV+zHenU/6Ol3T4l
K7lsObPJxfqWTTdTcnoqH0MrxQKViUZmJp+QNZe7CHwTfKN+xHqG1mCyLxJF6ewA
EhZRtcwe9ymaOgutoDKmxBUCgYARFGsNaoG+SbZHIDAm0q5kmlYmBxD3ndvcnIG4
VcU79gZtth+XrbvSexrsjDh0LFmdJNKQ0SMVfdzK6ADTCmXDPrlx2tbk7jWIv15X
go0dpK9EjnYB8eitamG1MRtSkgL1ueR8X4TWssFgJ16ajTbGNNJN0q3zVkAmSZ+4
emgGcwKBgQDe4sjFRuTpW2ey8TDSvHzs28h+A4AXvbCN5HXhoHz1kIoD3n9NdN0M
kWiBy/N2PhWf86/DYmpespPHelDvKAaWssgLYya/L7My3i5F0hkU3ibNYH8qzTzh
/wNA+cqDPm+lCA7cD8iyExLCfySr00trSlS54tU6npXuCoDJ2jtKQA==
-----END RSA PRIVATE KEY-----`

	newTestClient := func(serverURL string) *Client {
		c := New(1, []byte(testRSAKey))
		c.baseURL = serverURL
		return c
	}

	t.Run("returns blob entries only", func(t *testing.T) {
		mux := http.NewServeMux()

		// Installation token endpoint (required by ghinstallation transport)
		mux.HandleFunc("/app/installations/42/access_tokens", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "test-token",
				"expires_at": "2099-01-01T00:00:00Z",
			})
		})

		// Tree endpoint
		mux.HandleFunc("/repos/owner/repo/git/trees/main", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("recursive") != "1" {
				http.Error(w, "missing recursive=1", http.StatusBadRequest)
				return
			}
			if r.Header.Get("Accept") != "application/vnd.github+json" {
				http.Error(w, "missing Accept header", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"truncated": false,
				"tree": []map[string]interface{}{
					{"path": "README.md", "type": "blob", "size": 100},
					{"path": "src", "type": "tree", "size": 0},
					{"path": "src/main.go", "type": "blob", "size": 500},
				},
			})
		})

		srv := httptest.NewServer(mux)
		defer srv.Close()

		c := newTestClient(srv.URL)
		entries, err := c.GetTree(context.Background(), 42, "owner/repo", "main")
		if err != nil {
			t.Fatalf("GetTree() error = %v", err)
		}
		if len(entries) != 2 {
			t.Fatalf("GetTree() returned %d entries, want 2 (blobs only)", len(entries))
		}
		if entries[0].Path != "README.md" || entries[0].Type != "blob" || entries[0].Size != 100 {
			t.Errorf("entries[0] = %+v, want {README.md blob 100}", entries[0])
		}
		if entries[1].Path != "src/main.go" || entries[1].Type != "blob" || entries[1].Size != 500 {
			t.Errorf("entries[1] = %+v, want {src/main.go blob 500}", entries[1])
		}
	})

	t.Run("truncated tree logs warning", func(t *testing.T) {
		mux := http.NewServeMux()

		mux.HandleFunc("/app/installations/42/access_tokens", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "test-token",
				"expires_at": "2099-01-01T00:00:00Z",
			})
		})

		mux.HandleFunc("/repos/owner/repo/git/trees/main", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"truncated": true,
				"tree": []map[string]interface{}{
					{"path": "file.go", "type": "blob", "size": 200},
				},
			})
		})

		srv := httptest.NewServer(mux)
		defer srv.Close()

		c := newTestClient(srv.URL)
		entries, err := c.GetTree(context.Background(), 42, "owner/repo", "main")
		if err != nil {
			t.Fatalf("GetTree() error = %v", err)
		}
		// Should still return the partial results even when truncated
		if len(entries) != 1 {
			t.Fatalf("GetTree() returned %d entries, want 1", len(entries))
		}
	})

	t.Run("non-200 response returns error", func(t *testing.T) {
		mux := http.NewServeMux()

		mux.HandleFunc("/app/installations/42/access_tokens", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "test-token",
				"expires_at": "2099-01-01T00:00:00Z",
			})
		})

		mux.HandleFunc("/repos/owner/repo/git/trees/main", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		})

		srv := httptest.NewServer(mux)
		defer srv.Close()

		c := newTestClient(srv.URL)
		_, err := c.GetTree(context.Background(), 42, "owner/repo", "main")
		if err == nil {
			t.Fatal("GetTree() expected error for 404 response, got nil")
		}
	})
}

func TestFormatShadowIssueBody(t *testing.T) {
	body := FormatShadowIssueBody("IsmaelMartinez/teams-for-linux", 42, "Add dark mode support", "It would be great to have dark mode...")
	if !strings.Contains(body, "IsmaelMartinez/teams-for-linux#42") {
		t.Fatal("expected cross-repo issue reference")
	}
	if !strings.Contains(body, "Add dark mode support") {
		t.Fatal("expected original title")
	}
}
