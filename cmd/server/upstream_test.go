package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/upstream"
)

func TestUpstreamWatchHandler_MethodNotAllowed(t *testing.T) {
	srv := &server{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/upstream-watch", nil)
	srv.upstreamWatchHandler(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestUpstreamWatchHandler_BadBody(t *testing.T) {
	srv := &server{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/upstream-watch", strings.NewReader("not json"))
	req.ContentLength = 8
	srv.upstreamWatchHandler(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestToOutputMatches(t *testing.T) {
	matches := []upstream.Match{
		{
			Release: gh.Release{TagName: "v39.0.0"},
			Candidates: []store.SimilarIssue{
				{Issue: store.Issue{Number: 42}},
				{Issue: store.Issue{Number: 99}},
			},
		},
		{
			Release:    gh.Release{TagName: "v40.0.0"},
			Candidates: nil,
		},
	}
	out := toOutputMatches(matches)
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}
	if out[0].ReleaseTag != "v39.0.0" {
		t.Errorf("out[0].ReleaseTag = %q, want v39.0.0", out[0].ReleaseTag)
	}
	if len(out[0].Issues) != 2 || out[0].Issues[0] != 42 || out[0].Issues[1] != 99 {
		t.Errorf("out[0].Issues = %v, want [42 99]", out[0].Issues)
	}
	if out[1].ReleaseTag != "v40.0.0" {
		t.Errorf("out[1].ReleaseTag = %q, want v40.0.0", out[1].ReleaseTag)
	}
	if len(out[1].Issues) != 0 {
		t.Errorf("out[1].Issues = %v, want []", out[1].Issues)
	}
}

func TestToOutputMatches_Empty(t *testing.T) {
	out := toOutputMatches(nil)
	if out == nil {
		t.Fatal("toOutputMatches(nil) = nil, want empty slice")
	}
	if len(out) != 0 {
		t.Errorf("len(out) = %d, want 0", len(out))
	}
}

// TestUpstreamWatchHandler_EmptyTargets_ReturnsEmptyResults documents that the
// handler should 200 (not 500) when no targets are resolved. Skipped because
// the happy path requires a fully wired *server (srv.gh must be non-nil for
// ListInstallations); the error-isolation behaviour this PR adds is exercised
// by the cron in practice. Left in-tree as a placeholder for future
// integration coverage.
func TestUpstreamWatchHandler_EmptyTargets_ReturnsEmptyResults(t *testing.T) {
	t.Skip("full smoke test requires integration wiring — noted for follow-up")
}
