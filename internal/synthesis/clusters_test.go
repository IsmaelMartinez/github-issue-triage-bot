package synthesis

import (
	"math"
	"testing"
)

func TestGroupClusters(t *testing.T) {
	// Simulate 5 issues: 3 similar (audio), 2 different
	issues := []clusterCandidate{
		{number: 1, title: "Audio broken after update"},
		{number: 2, title: "No sound in calls"},
		{number: 3, title: "Audio crackling on Linux"},
		{number: 4, title: "Dark mode not working"},
		{number: 5, title: "Button alignment off"},
	}

	// Simulate similarity matrix where issues 0-2 are similar
	similar := map[[2]int]bool{
		{0, 1}: true, {0, 2}: true, {1, 2}: true,
	}

	groups := groupBySimilarity(issues, func(i, j int) bool {
		return similar[[2]int{i, j}] || similar[[2]int{j, i}]
	})

	// Should find at least one cluster of size 3
	found := false
	for _, g := range groups {
		if len(g) >= 3 {
			found = true
		}
	}
	if !found {
		t.Error("expected to find a cluster of 3+ issues")
	}
}

func TestCosineDistance(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
		tol  float64
	}{
		{
			name: "identical vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{1, 0, 0},
			want: 0.0,
			tol:  1e-9,
		},
		{
			name: "orthogonal vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{0, 1, 0},
			want: 1.0,
			tol:  1e-9,
		},
		{
			name: "opposite vectors",
			a:    []float32{1, 0, 0},
			b:    []float32{-1, 0, 0},
			want: 2.0,
			tol:  1e-9,
		},
		{
			name: "zero vector returns max distance",
			a:    []float32{0, 0, 0},
			b:    []float32{1, 0, 0},
			want: 1.0,
			tol:  1e-9,
		},
		{
			name: "scaled identical direction",
			a:    []float32{1, 2, 3},
			b:    []float32{2, 4, 6},
			want: 0.0,
			tol:  1e-6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineDistance(tt.a, tt.b)
			if math.Abs(got-tt.want) > tt.tol {
				t.Errorf("cosineDistance(%v, %v) = %f, want %f", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestClusterSynthesizerName(t *testing.T) {
	cs := NewClusterSynthesizer(nil)
	if cs.Name() != "cluster_detection" {
		t.Errorf("Name() = %q, want %q", cs.Name(), "cluster_detection")
	}
}
