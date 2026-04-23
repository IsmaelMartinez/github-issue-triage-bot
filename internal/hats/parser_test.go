package hats

import (
	"os"
	"reflect"
	"testing"
)

func TestParseFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/hats-example.md")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got.Hats) != 2 {
		t.Fatalf("len(Hats) = %d, want 2", len(got.Hats))
	}
	h := got.Hats[0]
	if h.Name != "display-session-media" {
		t.Errorf("name = %q", h.Name)
	}
	if h.Posture != "ambiguous-workaround-menu" {
		t.Errorf("posture = %q", h.Posture)
	}
	wantLabels := []string{"wayland", "screen-sharing"}
	if !reflect.DeepEqual(h.RetrievalLabels, wantLabels) {
		t.Errorf("labels = %v, want %v", h.RetrievalLabels, wantLabels)
	}
	wantKeywords := []string{"ozone", "xwayland"}
	if !reflect.DeepEqual(h.RetrievalBoostKeywords, wantKeywords) {
		t.Errorf("keywords = %v, want %v", h.RetrievalBoostKeywords, wantKeywords)
	}
	wantAnchors := []int{2169, 2138}
	if !reflect.DeepEqual(h.AnchorIssueNumbers, wantAnchors) {
		t.Errorf("anchors = %v, want %v", h.AnchorIssueNumbers, wantAnchors)
	}
}

func TestParseEmpty(t *testing.T) {
	_, err := Parse([]byte("# only preamble\n"))
	if err == nil {
		t.Error("expected error for no hats")
	}
}

func TestParseSeedFile(t *testing.T) {
	data, err := os.ReadFile("../../docs/hats-teams-for-linux.md")
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse seed: %v", err)
	}
	wantNames := []string{
		"display-session-media", "internal-regression-network",
		"tray-notifications", "upstream-blocked", "packaging",
		"configuration-cli", "enhancement-demand-gating",
		"auth-network-edge", "other",
	}
	if len(got.Hats) != len(wantNames) {
		t.Fatalf("got %d hats, want %d", len(got.Hats), len(wantNames))
	}
	for i, want := range wantNames {
		if got.Hats[i].Name != want {
			t.Errorf("hat[%d] = %q, want %q", i, got.Hats[i].Name, want)
		}
	}
}
