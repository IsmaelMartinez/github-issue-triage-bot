package synthesis

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/IsmaelMartinez/github-issue-triage-bot/internal/store"
)

// ClusterSynthesizer detects groups of semantically similar issues using
// embedding cosine distance and union-find clustering.
type ClusterSynthesizer struct {
	store     *store.Store
	threshold float64 // cosine distance threshold (lower = more similar)
	minSize   int
}

// NewClusterSynthesizer creates a ClusterSynthesizer with default thresholds.
func NewClusterSynthesizer(s *store.Store) *ClusterSynthesizer {
	return &ClusterSynthesizer{store: s, threshold: 0.35, minSize: 3}
}

func (c *ClusterSynthesizer) Name() string { return "cluster_detection" }

func (c *ClusterSynthesizer) Analyze(ctx context.Context, repo string, window time.Duration) ([]Finding, error) {
	since := time.Now().Add(-window)
	issues, err := c.store.RecentIssuesWithEmbeddings(ctx, repo, since)
	if err != nil {
		return nil, fmt.Errorf("fetching recent issues: %w", err)
	}
	if len(issues) < c.minSize {
		return nil, nil
	}

	candidates := make([]clusterCandidate, len(issues))
	for i, issue := range issues {
		candidates[i] = clusterCandidate{
			number:    issue.Number,
			title:     issue.Title,
			embedding: issue.Embedding,
		}
	}

	groups := groupBySimilarity(candidates, func(i, j int) bool {
		return cosineDistance(candidates[i].embedding, candidates[j].embedding) < c.threshold
	})

	var findings []Finding
	for _, group := range groups {
		if len(group) < c.minSize {
			continue
		}
		evidence := make([]string, len(group))
		titles := make([]string, len(group))
		for i, idx := range group {
			evidence[i] = fmt.Sprintf("#%d", candidates[idx].number)
			titles[i] = candidates[idx].title
		}

		// Check if there's ADR/roadmap coverage
		hasCoverage, refErr := c.store.CountReferencesTo(ctx, repo, "issue", evidence[0])
		if refErr != nil {
			return nil, fmt.Errorf("counting references for issue %s: %w", evidence[0], refErr)
		}

		severity := "warning"
		suggestion := fmt.Sprintf("%d issues opened in the last %d days share a common theme. Topics: %s.",
			len(group), int(window.Hours()/24), strings.Join(titles[:min(3, len(titles))], "; "))
		if hasCoverage == 0 {
			suggestion += " No existing ADR or roadmap item covers this area. Consider investigating whether this warrants a roadmap task."
			severity = "action_needed"
		}

		findings = append(findings, Finding{
			Type:       "cluster",
			Severity:   severity,
			Title:      fmt.Sprintf("Issue cluster: %s (%d issues)", titles[0], len(group)),
			Evidence:   evidence,
			Suggestion: suggestion,
		})
	}

	return findings, nil
}

type clusterCandidate struct {
	number    int
	title     string
	embedding []float32
}

// groupBySimilarity uses union-find with path compression to group items
// where isSimilar(i, j) returns true.
func groupBySimilarity(candidates []clusterCandidate, isSimilar func(i, j int) bool) [][]int {
	n := len(candidates)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		pa, pb := find(a), find(b)
		if pa != pb {
			parent[pa] = pb
		}
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if isSimilar(i, j) {
				union(i, j)
			}
		}
	}

	groups := make(map[int][]int)
	for i := 0; i < n; i++ {
		root := find(i)
		groups[root] = append(groups[root], i)
	}

	var result [][]int
	for _, g := range groups {
		result = append(result, g)
	}
	return result
}

// cosineDistance returns 1 - cosine_similarity(a, b).
// Returns 1.0 (maximum distance) for zero-norm vectors.
func cosineDistance(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 1.0
	}
	return 1.0 - (dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}
