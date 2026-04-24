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

func TestParseSeedFile_ExtractsFields(t *testing.T) {
	data, err := os.ReadFile("../../docs/hats-teams-for-linux.md")
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range got.Hats {
		if h.Name == "other" {
			continue
		}
		if h.Posture == "" {
			t.Errorf("hat %q has empty posture", h.Name)
		}
		if len(h.RetrievalLabels) == 0 {
			t.Errorf("hat %q has no retrieval labels", h.Name)
		}
		if len(h.RetrievalBoostKeywords) == 0 {
			t.Errorf("hat %q has no retrieval keywords", h.Name)
		}
	}

	display := got.Find("display-session-media")
	if display == nil {
		t.Fatal("no display-session-media hat")
	}
	if display.Posture != "ambiguous-workaround-menu" {
		t.Errorf("display posture = %q", display.Posture)
	}
	if !contains(display.RetrievalLabels, "wayland") {
		t.Errorf("display labels missing wayland: %v", display.RetrievalLabels)
	}
	if !contains(display.RetrievalBoostKeywords, "ozone") {
		t.Errorf("display keywords missing ozone: %v", display.RetrievalBoostKeywords)
	}

	reg := got.Find("internal-regression-network")
	if reg == nil {
		t.Fatal("no internal-regression-network hat")
	}
	if reg.Posture != "internal-regression" {
		t.Errorf("reg posture = %q", reg.Posture)
	}
	if !contains(reg.RetrievalLabels, "network") {
		t.Errorf("reg labels missing network: %v", reg.RetrievalLabels)
	}
	if !contains(reg.RetrievalBoostKeywords, "iframe") {
		t.Errorf("reg keywords missing iframe: %v", reg.RetrievalBoostKeywords)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestFirstSentenceKey_HandlesFormattingWrappers(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"`demand-gating-needed`.", "demand-gating-needed"},
		{"**demand-gating-needed**.", "demand-gating-needed"},
		{"*config-dependent*.", "config-dependent"},
		{"  `ambiguous-workaround-menu` by default", "ambiguous-workaround-menu"},
		{"internal-regression — narrative", "internal-regression"},
	}
	for _, c := range cases {
		if got := firstSentenceKey(c.in); got != c.want {
			t.Errorf("firstSentenceKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
