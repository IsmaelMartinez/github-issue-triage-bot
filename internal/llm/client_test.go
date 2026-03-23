package llm

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestEmbed_RetriesOnTransientError(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			// Simulate a 503 on first two attempts
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":"service unavailable"}`))
			return
		}
		resp := embeddingResponse{
			Embedding: embeddingValues{Values: []float32{0.1, 0.2, 0.3}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New("test-key", slog.Default())
	c.baseURL = srv.URL
	c.httpClient = srv.Client()

	vals, err := c.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if len(vals) != 3 {
		t.Fatalf("expected 3 values, got %d", len(vals))
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestEmbed_FailsAfterMaxRetries(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"always unavailable"}`))
	}))
	defer srv.Close()

	c := New("test-key", slog.Default())
	c.baseURL = srv.URL
	c.httpClient = srv.Client()

	_, err := c.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	if got := attempts.Load(); got != maxRetries {
		t.Fatalf("expected %d attempts, got %d", maxRetries, got)
	}
}

func TestGenerateJSON_RetriesOn429(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		resp := geminiResponse{
			Candidates: []candidate{
				{Content: content{Parts: []part{{Text: `{"result":"ok"}`}}}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New("test-key", slog.Default())
	c.baseURL = srv.URL
	c.httpClient = srv.Client()

	result, err := c.GenerateJSON(context.Background(), "test prompt", 0.5, 100)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if result != `{"result":"ok"}` {
		t.Fatalf("unexpected result: %s", result)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func TestGenerateJSON_NoRetryOn400(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	c := New("test-key", slog.Default())
	c.baseURL = srv.URL
	c.httpClient = srv.Client()

	_, err := c.GenerateJSON(context.Background(), "test prompt", 0.5, 100)
	if err == nil {
		t.Fatal("expected error on 400")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected 1 attempt (no retry on 400), got %d", got)
	}
}

func TestEmbed_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"unavailable"}`))
	}))
	defer srv.Close()

	c := New("test-key", slog.Default())
	c.baseURL = srv.URL
	c.httpClient = srv.Client()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := c.Embed(ctx, "hello")
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}
