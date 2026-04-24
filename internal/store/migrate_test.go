package store

import (
	"io/fs"
	"strings"
	"testing"
)

// TestEmbeddedMigrations guards against the runtime shipping without a
// migration file. Running `go build` without the SQL files copied would
// silently produce a binary that skips schema changes.
func TestEmbeddedMigrations(t *testing.T) {
	entries, err := fs.ReadDir(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	if len(names) < 10 {
		t.Fatalf("expected at least 10 embedded migrations, got %d: %v", len(names), names)
	}
	want := "015_triage_session_pending_promotion.sql"
	found := false
	for _, n := range names {
		if n == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("migration %s not embedded; got %v", want, names)
	}
}
