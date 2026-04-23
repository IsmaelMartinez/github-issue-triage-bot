package hats

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoader_CachesWithinTTL(t *testing.T) {
	var calls int32
	fetch := func() ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return fixtureBytes(t), nil
	}
	l := NewLoader(fetch, 100*time.Millisecond)
	if _, err := l.Get(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Get(); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (second Get should hit cache)", got)
	}
	time.Sleep(150 * time.Millisecond)
	if _, err := l.Get(); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("calls = %d, want 2 (TTL expired)", got)
	}
}

func TestLoader_FetchErrorReturnsStaleIfAvailable(t *testing.T) {
	var calls int32
	fetch := func() ([]byte, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return fixtureBytes(t), nil
		}
		return nil, errors.New("network down")
	}
	l := NewLoader(fetch, 10*time.Millisecond)
	first, err := l.Get()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	second, err := l.Get()
	if err != nil {
		t.Fatalf("want stale-cache fallback, got err %v", err)
	}
	if len(first.Hats) != len(second.Hats) {
		t.Errorf("stale cache should match first result")
	}
}

func TestLoader_NoStaleCacheReturnsError(t *testing.T) {
	fetch := func() ([]byte, error) {
		return nil, errors.New("network down")
	}
	l := NewLoader(fetch, time.Second)
	_, err := l.Get()
	if err == nil {
		t.Error("expected error when no cache exists and fetch fails")
	}
}

func TestLoader_Invalidate(t *testing.T) {
	var calls int32
	fetch := func() ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return fixtureBytes(t), nil
	}
	l := NewLoader(fetch, time.Hour)
	if _, err := l.Get(); err != nil {
		t.Fatal(err)
	}
	l.Invalidate()
	if _, err := l.Get(); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("calls = %d after invalidate, want 2", got)
	}
}

func fixtureBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/hats-example.md")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return data
}
