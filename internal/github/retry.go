package github

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// retryTransport wraps a http.RoundTripper with bounded exponential-backoff
// retries for transient network failures (TLS handshake timeouts, connection
// resets, 502/503/504). It is intended to absorb brief api.github.com hiccups
// that otherwise drop lgtm signals.
type retryTransport struct {
	base     http.RoundTripper
	attempts int
	baseWait time.Duration
}

func newRetryTransport(base http.RoundTripper) *retryTransport {
	return &retryTransport{base: base, attempts: 3, baseWait: 200 * time.Millisecond}
}

func (rt *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Buffer the body so we can replay it on retry. GitHub API payloads are small.
	var body []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
		body = b
	}

	var lastResp *http.Response
	var lastErr error
	for attempt := 0; attempt < rt.attempts; attempt++ {
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		resp, err := rt.base.RoundTrip(req)
		if !shouldRetry(resp, err) {
			return resp, err
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		lastResp, lastErr = resp, err
		if attempt < rt.attempts-1 {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(rt.baseWait * (1 << attempt)):
			}
		}
	}
	return lastResp, lastErr
}

func shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return true
		}
		msg := err.Error()
		if strings.Contains(msg, "TLS handshake timeout") ||
			strings.Contains(msg, "connection reset") ||
			strings.Contains(msg, "EOF") {
			return true
		}
		return false
	}
	if resp != nil {
		switch resp.StatusCode {
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
	}
	return false
}
