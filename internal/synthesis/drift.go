package synthesis

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// DriftSynthesizer detects decision drift and roadmap staleness by correlating
// journal events with documentation in the vector store.
type DriftSynthesizer struct {
	store *store.Store
}

// NewDriftSynthesizer creates a drift detection synthesizer.
func NewDriftSynthesizer(s *store.Store) *DriftSynthesizer {
	return &DriftSynthesizer{store: s}
}

// Name returns the synthesizer identifier.
func (d *DriftSynthesizer) Name() string {
	return "drift_detection"
}

// Analyze runs both staleness and drift detection within the given time window.
func (d *DriftSynthesizer) Analyze(ctx context.Context, repo string, window time.Duration) ([]Finding, error) {
	var findings []Finding

	stale, err := d.detectStaleness(ctx, repo, window)
	if err != nil {
		return nil, fmt.Errorf("staleness detection: %w", err)
	}
	findings = append(findings, stale...)

	drift, err := d.detectDrift(ctx, repo, window)
	if err != nil {
		return nil, fmt.Errorf("drift detection: %w", err)
	}
	findings = append(findings, drift...)

	return findings, nil
}

// Staleness age thresholds: ADRs are architectural decisions that change
// infrequently, so 90 days is a reasonable minimum before flagging. Roadmap
// items are expected to evolve more often.
const (
	adrStalenessAge     = 90 * 24 * time.Hour
	roadmapStalenessAge = 30 * 24 * time.Hour
)

// detectStaleness finds roadmap and ADR documents that haven't been updated
// within their doc-type-specific age threshold but ARE actively referenced in
// recent issues or PRs. Documents that are settled and not being discussed are
// left alone — they don't need attention just because they haven't been edited
// recently.
func (d *DriftSynthesizer) detectStaleness(ctx context.Context, repo string, window time.Duration) ([]Finding, error) {
	docs, err := d.store.ListDocumentsByType(ctx, repo, []string{"roadmap", "adr"})
	if err != nil {
		return nil, fmt.Errorf("list docs: %w", err)
	}

	since := time.Now().Add(-window)
	var findings []Finding

	for _, doc := range docs {
		if !isSignificantlyStale(doc, time.Now()) {
			continue
		}

		// Check if this document has been referenced recently. Use the doc
		// title as-is (it's recorded as source_id during ingest) and also
		// try the ADR-NNN pattern (used as target_id when issues reference ADRs).
		refCount, err := d.store.CountRecentReferencesInvolving(ctx, repo, doc.Title, since)
		if err != nil {
			return nil, fmt.Errorf("count recent refs for doc %d: %w", doc.ID, err)
		}
		if refCount == 0 {
			// Also check for ADR-NNN pattern references extracted from the title.
			if adrRef := extractADRRef(doc.Title); adrRef != "" {
				adrRefCount, err := d.store.CountRecentReferencesInvolving(ctx, repo, adrRef, since)
				if err != nil {
					return nil, fmt.Errorf("count recent ADR refs for doc %d: %w", doc.ID, err)
				}
				refCount = adrRefCount
			}
		}

		// Only flag if the document is actively being discussed but not updated.
		// Require 2+ references to avoid noise from single incidental mentions.
		if refCount >= 2 {
			findings = append(findings, Finding{
				Type:     "staleness",
				Severity: "info",
				Title:    fmt.Sprintf("%s %q is referenced in recent activity but has not been updated", strings.ToUpper(doc.DocType), doc.Title),
				Evidence: []string{
					fmt.Sprintf("doc_id=%d", doc.ID),
					fmt.Sprintf("last_updated=%s", doc.UpdatedAt.Format("2006-01-02")),
					fmt.Sprintf("recent_references=%d", refCount),
				},
				Suggestion: fmt.Sprintf("Review whether this %s needs updating to reflect recent discussions.", doc.DocType),
			})
		}
	}

	return findings, nil
}

// extractADRRef extracts an "ADR-NNN" reference from a document title, if present.
func extractADRRef(title string) string {
	match := adrTitleRe.FindString(title)
	return match
}

var adrTitleRe = regexp.MustCompile(`ADR-\d+`)

// isSignificantlyStale returns true if the document has not been updated within
// its doc-type-specific staleness threshold (90 days for ADRs, 30 days for
// roadmap items).
func isSignificantlyStale(doc store.Document, now time.Time) bool {
	age := adrStalenessAge
	if doc.DocType == "roadmap" {
		age = roadmapStalenessAge
	}
	cutoff := now.Add(-age)
	return doc.UpdatedAt.Before(cutoff)
}

// detectDrift finds merged PRs that touch areas covered by ADRs but where the
// ADR has not been updated recently, suggesting the decision may be drifting.
func (d *DriftSynthesizer) detectDrift(ctx context.Context, repo string, window time.Duration) ([]Finding, error) {
	since := time.Now().Add(-window)

	events, err := d.store.ListEvents(ctx, repo, since, []string{"pr_merged"}, 100)
	if err != nil {
		return nil, fmt.Errorf("list pr events: %w", err)
	}
	if len(events) == 0 {
		return nil, nil
	}

	adrs, err := d.store.ListDocumentsByType(ctx, repo, []string{"adr"})
	if err != nil {
		return nil, fmt.Errorf("list adrs: %w", err)
	}
	if len(adrs) == 0 {
		return nil, nil
	}

	// Build a map from area keywords to ADR documents.
	adrsByArea := buildADRAreaIndex(adrs)

	cutoff := since
	var findings []Finding

	for _, event := range events {
		prAreas := extractAreas(event)
		for _, area := range prAreas {
			matchedADRs, ok := adrsByArea[strings.ToLower(area)]
			if !ok {
				continue
			}
			for _, adr := range matchedADRs {
				if adr.UpdatedAt.Before(cutoff) {
					findings = append(findings, Finding{
						Type:     "drift",
						Severity: "warning",
						Title:    fmt.Sprintf("PR %s touches area %q covered by ADR %q", event.SourceRef, area, adr.Title),
						Evidence: []string{
							fmt.Sprintf("pr=%s", event.SourceRef),
							fmt.Sprintf("adr_id=%d", adr.ID),
							fmt.Sprintf("adr_updated=%s", adr.UpdatedAt.Format("2006-01-02")),
						},
						Suggestion: fmt.Sprintf("ADR %q may need updating to reflect changes introduced by this PR.", adr.Title),
					})
				}
			}
		}
	}

	return findings, nil
}

// extractAreas returns area keywords from an event. It prefers the Areas field
// and falls back to extracting from metadata["changed_files"].
func extractAreas(event store.RepoEvent) []string {
	if len(event.Areas) > 0 {
		return event.Areas
	}

	changedFiles, ok := event.Metadata["changed_files"]
	if !ok {
		return nil
	}

	// changed_files may be a []any (from JSON unmarshal) or a string.
	switch v := changedFiles.(type) {
	case []any:
		areas := make([]string, 0, len(v))
		for _, f := range v {
			if s, ok := f.(string); ok {
				areas = append(areas, extractAreaFromPath(s))
			}
		}
		return deduplicate(areas)
	case string:
		parts := strings.Split(v, ",")
		areas := make([]string, 0, len(parts))
		for _, p := range parts {
			areas = append(areas, extractAreaFromPath(strings.TrimSpace(p)))
		}
		return deduplicate(areas)
	}

	return nil
}

// extractAreaFromPath returns the top-level directory from a file path as the area keyword.
// For paths without directories, the filename itself is returned.
func extractAreaFromPath(path string) string {
	path = strings.TrimPrefix(path, "/")
	if idx := strings.Index(path, "/"); idx > 0 {
		return path[:idx]
	}
	return path
}

// driftStopWords contains common English words and generic software terms that
// are too broad to use as area keywords for drift detection.
var driftStopWords = map[string]bool{
	// Common English words
	"the": true, "and": true, "for": true, "use": true, "add": true,
	"new": true, "via": true, "may": true, "can": true, "not": true,
	"all": true, "how": true, "why": true, "was": true, "are": true,
	"has": true, "had": true, "but": true, "its": true, "our": true,
	"with": true, "from": true, "that": true, "this": true, "will": true,
	"when": true, "into": true, "over": true, "also": true, "more": true,
	"than": true, "been": true, "have": true, "does": true, "each": true,
	"make": true, "like": true, "should": true, "would": true, "could": true,
	// Common software terms that are too generic
	"adr": true, "doc": true, "docs": true, "api": true, "app": true,
	"src": true, "cmd": true, "pkg": true, "lib": true, "test": true,
	"data": true, "file": true, "code": true, "config": true,
	"internal": true,
}

// buildADRAreaIndex maps lowercase area keywords extracted from ADR titles and
// content to the ADR documents that cover those areas.
func buildADRAreaIndex(adrs []store.Document) map[string][]store.Document {
	index := make(map[string][]store.Document)
	for _, adr := range adrs {
		// Extract areas from the ADR title (split on common separators).
		words := tokenize(adr.Title)
		for _, w := range words {
			key := strings.ToLower(w)
			if len(key) > 2 && !driftStopWords[key] {
				index[key] = append(index[key], adr)
			}
		}
		// Also check metadata for explicit area tags.
		if areas, ok := adr.Metadata["areas"]; ok {
			if areaList, ok := areas.([]any); ok {
				for _, a := range areaList {
					if s, ok := a.(string); ok {
						index[strings.ToLower(s)] = append(index[strings.ToLower(s)], adr)
					}
				}
			}
		}
	}
	return index
}

// tokenize splits text into word tokens, stripping common punctuation.
func tokenize(text string) []string {
	replacer := strings.NewReplacer(":", " ", "-", " ", "_", " ", "/", " ", "(", " ", ")", " ", ",", " ")
	return strings.Fields(replacer.Replace(text))
}

// deduplicate removes duplicate strings from a slice, preserving order.
func deduplicate(items []string) []string {
	seen := make(map[string]bool, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}
