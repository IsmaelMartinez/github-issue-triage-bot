package codenav

import (
	"path/filepath"
	"strings"
)

// Source file extensions worth including in code navigation.
var sourceExtensions = map[string]bool{
	".js": true, ".ts": true, ".jsx": true, ".tsx": true,
	".go": true, ".py": true, ".rb": true, ".rs": true,
	".java": true, ".kt": true, ".swift": true,
	".c": true, ".h": true, ".cpp": true,
	".sh": true, ".bash": true,
	".json": true, ".yml": true, ".yaml": true, ".toml": true,
}

// Directories to exclude from code navigation.
var excludeDirs = []string{
	"node_modules/", "vendor/", ".git/", "dist/", "build/",
	"assets/", "fonts/", "icons/",
	"tests/", "test/", "__tests__/", "spec/",
	".github/", "docs-site/", "docs/",
}

// Filenames to exclude.
var excludeFiles = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"go.sum": true, "Cargo.lock": true,
}

// FilterSourceFiles returns only source code paths, excluding assets, tests, vendored code, lock files, and docs.
func FilterSourceFiles(paths []string) []string {
	var result []string
	for _, p := range paths {
		if excludeFiles[filepath.Base(p)] {
			continue
		}
		ext := filepath.Ext(p)
		if !sourceExtensions[ext] {
			continue
		}
		excluded := false
		for _, dir := range excludeDirs {
			if strings.Contains(p, dir) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		result = append(result, p)
	}
	return result
}

// FormatTreeForLLM formats file paths as a compact list for LLM prompts.
func FormatTreeForLLM(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Source files:\n")
	for _, p := range paths {
		sb.WriteString("  ")
		sb.WriteString(p)
		sb.WriteByte('\n')
	}
	return sb.String()
}
