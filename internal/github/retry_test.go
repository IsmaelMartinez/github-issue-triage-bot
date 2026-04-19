package github

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRoundTripper struct {
	responses []fakeResp
	calls     int32
}

type fakeResp struct {
	status int
	err    error
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	i := int(atomic.AddInt32(&f.calls, 1)) - 1
	if i >= len(f.responses) {
		i = len(f.responses) - 1
	}
	r := f.responses[i]
	if r.err != nil {
		return nil, r.err
	}
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "TLS handshake timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestRetryTransport_RetriesOnTLSTimeoutThenSucceeds(t *testing.T) {
	base := &fakeRoundTripper{responses: []fakeResp{
		{err: timeoutErr{}},
		{err: timeoutErr{}},
		{status: 201},
	}}
	rt := &retryTransport{base: base, attempts: 3, baseWait: 1 * time.Millisecond}

	req, _ := http.NewRequest(http.MethodPost, "https://api.example.com/x", strings.NewReader(`{"a":1}`))
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("want success after retries, got %v", err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("want 201, got %d", resp.StatusCode)
	}
	if base.calls != 3 {
		t.Fatalf("want 3 calls, got %d", base.calls)
	}
}

func TestRetryTransport_DoesNotRetryOn4xx(t *testing.T) {
	base := &fakeRoundTripper{responses: []fakeResp{{status: 422}}}
	rt := &retryTransport{base: base, attempts: 3, baseWait: 1 * time.Millisecond}

	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/x", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 422 {
		t.Fatalf("want 422, got %d", resp.StatusCode)
	}
	if base.calls != 1 {
		t.Fatalf("want 1 call, got %d", base.calls)
	}
}

func TestRetryTransport_GivesUpAfterMaxAttempts(t *testing.T) {
	base := &fakeRoundTripper{responses: []fakeResp{{err: timeoutErr{}}}}
	rt := &retryTransport{base: base, attempts: 3, baseWait: 1 * time.Millisecond}

	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/x", nil)
	_, err := rt.RoundTrip(req)
	if err == nil {
		t.Fatalf("want error after max attempts")
	}
	if base.calls != 3 {
		t.Fatalf("want 3 calls, got %d", base.calls)
	}
}

func TestRetryTransport_RetriesOn503(t *testing.T) {
	base := &fakeRoundTripper{responses: []fakeResp{
		{status: 503},
		{status: 200},
	}}
	rt := &retryTransport{base: base, attempts: 3, baseWait: 1 * time.Millisecond}

	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/x", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if base.calls != 2 {
		t.Fatalf("want 2 calls, got %d", base.calls)
	}
}

func TestRetryTransport_ReplaysBody(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, string(b))
		if len(seen) == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	rt := &retryTransport{base: http.DefaultTransport, attempts: 3, baseWait: 1 * time.Millisecond}
	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{"lgtm":true}`))
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if len(seen) != 2 || seen[0] != `{"lgtm":true}` || seen[1] != `{"lgtm":true}` {
		t.Fatalf("want body replayed identically across retries, got %q", seen)
	}
}

func TestShouldRetry(t *testing.T) {
	cases := []struct {
		name string
		resp *http.Response
		err  error
		want bool
	}{
		{"tls timeout", nil, timeoutErr{}, true},
		{"conn reset", nil, errors.New("read: connection reset by peer"), true},
		{"eof", nil, errors.New("unexpected EOF"), true},
		{"random non-retryable error", nil, errors.New("permission denied"), false},
		{"502", &http.Response{StatusCode: 502}, nil, true},
		{"503", &http.Response{StatusCode: 503}, nil, true},
		{"504", &http.Response{StatusCode: 504}, nil, true},
		{"401", &http.Response{StatusCode: 401}, nil, false},
		{"200", &http.Response{StatusCode: 200}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRetry(tc.resp, tc.err); got != tc.want {
				t.Fatalf("shouldRetry = %v, want %v", got, tc.want)
			}
		})
	}
}
