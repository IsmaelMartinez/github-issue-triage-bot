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

// maxBufferedBody caps the request body size we buffer for replay.
// GitHub API request payloads are small (comment bodies, issue fields); 1 MiB
// leaves ample headroom while preventing a malicious caller from exhausting
// memory through the retry layer.
const maxBufferedBody = 1 << 20

func (rt *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Buffer the body so we can replay it on retry. Bounded by maxBufferedBody.
	var body []byte
	if req.Body != nil {
		b, err := io.ReadAll(io.LimitReader(req.Body, maxBufferedBody))
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
		body = b
	}

	for attempt := 0; attempt < rt.attempts; attempt++ {
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		resp, err := rt.base.RoundTrip(req)
		// Final attempt: return whatever we got, including the open body,
		// so the caller can read it.
		if !shouldRetry(resp, err) || attempt == rt.attempts-1 {
			return resp, err
		}
		if resp != nil {
			_ = resp.Body.Close()
		}

		timer := time.NewTimer(rt.baseWait * (1 << attempt))
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
	return nil, nil // unreachable: the loop always returns on the final attempt
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
