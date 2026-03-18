package config

import (
	"testing"
	"time"
)

func TestConfigCache(t *testing.T) {
	calls := 0
	fetcher := func() ([]byte, error) {
		calls++
		return []byte(`{"capabilities":{"synthesis":true}}`), nil
	}

	cache := NewCache(1*time.Hour, fetcher)

	cfg1, err := cache.Get()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg1.Capabilities.Synthesis {
		t.Error("expected synthesis enabled")
	}
	if calls != 1 {
		t.Errorf("expected 1 fetch call, got %d", calls)
	}

	_, err = cache.Get()
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("expected still 1 fetch call, got %d", calls)
	}
}

func TestConfigCacheReturnsDefaultOnNilContent(t *testing.T) {
	fetcher := func() ([]byte, error) {
		return nil, nil
	}
	cache := NewCache(1*time.Hour, fetcher)
	cfg, err := cache.Get()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Capabilities.Synthesis {
		t.Error("default config should have synthesis disabled")
	}
	if !cfg.Capabilities.Triage {
		t.Error("default config should have triage enabled")
	}
}
