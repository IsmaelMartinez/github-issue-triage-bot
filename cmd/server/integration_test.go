//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

func TestIngestEndpointHTTP(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/triage_bot?sslmode=disable"
	}

	pool, err := store.ConnectPool(t.Context(), dbURL)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()
	s := store.New(pool)

	repo := "integration-test/ingest-http"
	t.Cleanup(func() {
		_, _ = pool.Exec(t.Context(), "DELETE FROM repo_events WHERE repo = $1", repo)
	})

	// Build the ingest handler directly (same as main.go wires it)
	secret := "test-secret"
	mux := http.NewServeMux()
	mux.HandleFunc("/ingest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !validateIngestAuth(r.Header.Get("Authorization"), secret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
		var events []store.RepoEvent
		if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if err := s.RecordEvents(r.Context(), events); err != nil {
			http.Error(w, "ingest failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ingested":` + json.Number(itoa(len(events))).String() + `}`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Test: unauthorized request
	resp, err := http.Post(ts.URL+"/ingest", "application/json", bytes.NewReader([]byte(`[]`)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no auth: status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// Test: authorized request with events
	events := []store.RepoEvent{
		{Repo: repo, EventType: "pr_merged", SourceRef: "#99", Summary: "HTTP test PR"},
	}
	body, _ := json.Marshal(events)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/ingest", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("authorized: status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify event was stored
	count, err := s.CountEvents(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("CountEvents = %d, want 1", count)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
