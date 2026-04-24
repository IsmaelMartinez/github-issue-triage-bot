package store

import (
	"sort"
	"strings"
)

// ApplyHatBoost rescales distances downward (closer) for documents whose
// title or content contains any of the boost keywords (case-insensitive).
// Returns a new slice ordered ascending by the adjusted distance.
//
// This is a soft rerank — documents without a keyword match keep their
// original distance. The adjusted distance is clamped at 0.
//
// If keywords is empty or boost <= 0, the original slice is returned
// unchanged (same ordering, same distances).
func ApplyHatBoost(docs []SimilarDocument, keywords []string, boost float64) []SimilarDocument {
	if len(keywords) == 0 || boost <= 0 {
		return docs
	}
	lowered := make([]string, len(keywords))
	for i, k := range keywords {
		lowered[i] = strings.ToLower(k)
	}
	type scored struct {
		doc      SimilarDocument
		adjusted float64
	}
	out := make([]scored, len(docs))
	for i, d := range docs {
		adj := d.Distance
		// Probe title + content for any keyword. Use ToLower for a cheap
		// case-insensitive match.
		hay := strings.ToLower(hayFor(d))
		for _, k := range lowered {
			if strings.Contains(hay, k) {
				adj -= boost
				break
			}
		}
		if adj < 0 {
			adj = 0
		}
		out[i] = scored{doc: d, adjusted: adj}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].adjusted < out[j].adjusted })
	result := make([]SimilarDocument, len(out))
	for i, s := range out {
		result[i] = s.doc
		result[i].Distance = s.adjusted
	}
	return result
}

// hayFor returns the text blob to match keywords against.
// Consolidated to one place so the caller doesn't leak knowledge of
// which SimilarDocument fields are searched.
func hayFor(d SimilarDocument) string {
	return d.Title + " " + d.Content
}
