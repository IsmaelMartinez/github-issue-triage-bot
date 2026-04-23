package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBriefPreviewHandler_MethodNotAllowed(t *testing.T) {
	srv := &server{}
	rr := httptest.NewRecorder()
	srv.briefPreviewHandler(rr, httptest.NewRequest(http.MethodGet, "/brief-preview", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestBriefPreviewHandler_InvalidJSON(t *testing.T) {
	srv := &server{}
	rr := httptest.NewRecorder()
	srv.briefPreviewHandler(rr, httptest.NewRequest(http.MethodPost, "/brief-preview", bytes.NewReader([]byte("not json"))))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestBriefPreviewHandler_MissingFields(t *testing.T) {
	srv := &server{}
	rr := httptest.NewRecorder()
	srv.briefPreviewHandler(rr, httptest.NewRequest(http.MethodPost, "/brief-preview", strings.NewReader(`{}`)))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestBriefPreviewHandler_RejectsUnknownRepo(t *testing.T) {
	srv := &server{allowedRepos: map[string]bool{}}
	rr := httptest.NewRecorder()
	body := strings.NewReader(`{"repo":"owner/repo","issue_number":1}`)
	req := httptest.NewRequest(http.MethodPost, "/brief-preview", body)
	srv.briefPreviewHandler(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

func TestClassName(t *testing.T) {
	if got := className(nil, ""); got != "other" {
		t.Errorf("className(nil, \"\") = %q, want other", got)
	}
	if got := className(nil, "display-session-media"); got != "display-session-media" {
		t.Errorf("className(nil, requested) should echo requested, got %q", got)
	}
}

func TestExtractSymptomKeywords(t *testing.T) {
	body := "Screen sharing fails under Wayland; notification toast missing too."
	got := extractSymptomKeywords(body)
	// Must hit at least "screen", "wayland", "notification"; set membership
	// rather than exact order so adding candidates later does not break this.
	want := map[string]bool{"screen": false, "wayland": false, "notification": false}
	for _, k := range got {
		if _, ok := want[k]; ok {
			want[k] = true
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("extractSymptomKeywords missing %q in %v", k, got)
		}
	}
}

func TestAllDocTypes(t *testing.T) {
	got := allDocTypes()
	if len(got) != 7 {
		t.Errorf("allDocTypes length = %d, want 7", len(got))
	}
}

func TestWorkingVersionRe(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"works in v1.2.3 and breaks on latest", "1.2.3"},
		{"This worked in 2.5 but not now", "2.5"},
		{"no version here", ""},
	}
	for _, tc := range cases {
		m := workingVersionRe.FindStringSubmatch(tc.in)
		if tc.want == "" {
			if m != nil {
				t.Errorf("input %q: expected no match, got %v", tc.in, m)
			}
			continue
		}
		if m == nil || m[1] != tc.want {
			t.Errorf("input %q: got %v, want capture %q", tc.in, m, tc.want)
		}
	}
}
