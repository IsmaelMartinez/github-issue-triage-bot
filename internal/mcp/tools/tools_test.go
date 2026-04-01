package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchJSONAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("missing or wrong auth header: %q", r.Header.Get("Authorization"))
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"ok": true}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	_, err := fetchJSON(server.URL, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
}

func TestFetchJSONNoAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"ok": true}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	_, err := fetchJSON(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
}

func TestFetchJSONHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := fetchJSON(server.URL, "")
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestPostJSONMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"ok": true}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	_, err := postJSON(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
}
