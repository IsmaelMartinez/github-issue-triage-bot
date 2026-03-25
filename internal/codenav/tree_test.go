package codenav

import (
	"strings"
	"testing"
)

func TestFilterSourceFiles(t *testing.T) {
	entries := []string{
		"app/index.js",
		"app/config/index.js",
		"app/assets/icon.png",
		"package-lock.json",
		"node_modules/foo/index.js",
		"tests/unit/foo.test.js",
		".github/workflows/build.yml",
		"docs-site/docs/adr/001.md",
		"app/browser/tools/tokenCache.js",
	}
	filtered := FilterSourceFiles(entries)
	expected := []string{
		"app/index.js",
		"app/config/index.js",
		"app/browser/tools/tokenCache.js",
	}
	if len(filtered) != len(expected) {
		t.Fatalf("expected %d files, got %d: %v", len(expected), len(filtered), filtered)
	}
}

func TestFormatTreeForLLM(t *testing.T) {
	paths := []string{
		"app/index.js",
		"app/config/index.js",
		"app/browser/tools/tokenCache.js",
	}
	result := FormatTreeForLLM(paths)
	if result == "" {
		t.Fatal("expected non-empty tree string")
	}
	if !strings.Contains(result, "tokenCache.js") {
		t.Error("expected tokenCache.js in output")
	}
}

func TestFilterSourceFiles_Empty(t *testing.T) {
	filtered := FilterSourceFiles(nil)
	if len(filtered) != 0 {
		t.Fatalf("expected 0 files, got %d", len(filtered))
	}
}

func TestFormatTreeForLLM_Empty(t *testing.T) {
	result := FormatTreeForLLM(nil)
	if result != "" {
		t.Fatal("expected empty string for nil input")
	}
}
