package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
