package config

import (
	"sync"
	"time"
)

// FetchFunc fetches the raw config file content. Returns nil, nil if the file doesn't exist.
type FetchFunc func() ([]byte, error)

// Cache provides a TTL-based cache for butler config.
type Cache struct {
	mu      sync.RWMutex
	cfg     ButlerConfig
	fetched bool
	fetchAt time.Time
	ttl     time.Duration
	fetch   FetchFunc
}

// NewCache creates a new config cache with the given TTL and fetch function.
func NewCache(ttl time.Duration, fetch FetchFunc) *Cache {
	return &Cache{ttl: ttl, fetch: fetch}
}

// Get returns the cached config, fetching it if the cache is stale or empty.
func (c *Cache) Get() (ButlerConfig, error) {
	c.mu.RLock()
	if c.fetched && time.Since(c.fetchAt) < c.ttl {
		cfg := c.cfg
		c.mu.RUnlock()
		return cfg, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.fetched && time.Since(c.fetchAt) < c.ttl {
		return c.cfg, nil
	}

	data, err := c.fetch()
	if err != nil {
		// On fetch error, return stale cache if available, otherwise defaults.
		// Do NOT update fetchAt — retry on next call.
		if c.fetched {
			return c.cfg, nil
		}
		return DefaultConfig(), nil
	}

	if data == nil {
		c.cfg = DefaultConfig()
		c.fetched = true
		c.fetchAt = time.Now()
	} else {
		cfg, parseErr := Parse(data)
		if parseErr != nil {
			// Parse error: return defaults but don't cache — retry next call.
			if c.fetched {
				return c.cfg, nil
			}
			return DefaultConfig(), nil
		}
		c.cfg = cfg
		c.fetched = true
		c.fetchAt = time.Now()
	}
	return c.cfg, nil
}

// Invalidate forces the next Get() call to re-fetch.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	c.fetched = false
	c.mu.Unlock()
}
