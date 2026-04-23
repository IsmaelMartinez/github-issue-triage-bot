package hats

import (
	"context"
	"sync"
	"time"
)

// FetchFunc returns the raw bytes of hats.md or an error.
type FetchFunc func() ([]byte, error)

// Loader caches a parsed Taxonomy with a TTL and returns stale results
// if a fetch fails after TTL expiry.
type Loader struct {
	mu      sync.RWMutex
	fetch   FetchFunc
	ttl     time.Duration
	cached  *Taxonomy
	fetched time.Time
}

// NewLoader returns a Loader that fetches via f and caches for ttl.
func NewLoader(f FetchFunc, ttl time.Duration) *Loader {
	return &Loader{fetch: f, ttl: ttl}
}

// Get returns the current taxonomy, refreshing if the cache is expired.
// On fetch error, returns the stale cached taxonomy if one exists;
// otherwise returns the fetch error.
func (l *Loader) Get() (Taxonomy, error) {
	l.mu.RLock()
	fresh := l.cached != nil && time.Since(l.fetched) < l.ttl
	if fresh {
		cached := *l.cached
		l.mu.RUnlock()
		return cached, nil
	}
	l.mu.RUnlock()

	data, err := l.fetch()
	if err != nil {
		l.mu.RLock()
		defer l.mu.RUnlock()
		if l.cached != nil {
			return *l.cached, nil
		}
		return Taxonomy{}, err
	}
	tax, err := Parse(data)
	if err != nil {
		return Taxonomy{}, err
	}

	l.mu.Lock()
	l.cached = &tax
	l.fetched = time.Now()
	l.mu.Unlock()
	return tax, nil
}

// Invalidate forces the next Get() to re-fetch.
func (l *Loader) Invalidate() {
	l.mu.Lock()
	l.cached = nil
	l.mu.Unlock()
}

// ContentFetcher is the subset of the github client this package needs.
// Defined here to avoid a direct dependency on the github package.
type ContentFetcher interface {
	GetFileContents(ctx context.Context, installationID int64, repo, path string) ([]byte, error)
}

// GitHubFetchFunc wires a ContentFetcher into a FetchFunc for a given
// repo and path. Each call to the returned FetchFunc issues one
// GetFileContents call — the caller owns retry/timeout policy.
func GitHubFetchFunc(ctx context.Context, f ContentFetcher, installationID int64, repo, path string) FetchFunc {
	return func() ([]byte, error) {
		return f.GetFileContents(ctx, installationID, repo, path)
	}
}
