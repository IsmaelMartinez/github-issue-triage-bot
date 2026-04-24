package regression

import (
	"context"
	"errors"
	"testing"

	gh "github.com/IsmaelMartinez/github-issue-triage-bot/internal/github"
)

type fakeClient struct {
	prs []gh.MergedPR
	err error
}

func (f *fakeClient) ListMergedPRsBetween(ctx context.Context, installationID int64, repo, from, to string) ([]gh.MergedPR, error) {
	return f.prs, f.err
}

func TestDiff_FiltersByKeyword(t *testing.T) {
	prs := []gh.MergedPR{
		{Number: 1, Title: "fix iframe reload on network failure", Body: "scoped to top-frame only"},
		{Number: 2, Title: "bump electron to 39.8.2", Body: ""},
		{Number: 3, Title: "update README", Body: "no code changes"},
	}
	c := &fakeClient{prs: prs}
	d := NewDiff(c)
	got, err := d.Run(context.Background(), 1, "repo/name", "v2.7.5", "v2.7.8",
		[]string{"iframe", "reload", "network"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Number != 1 {
		t.Errorf("got %v, want single PR #1", got)
	}
}

func TestDiff_EmptyKeywordsReturnsAll(t *testing.T) {
	prs := []gh.MergedPR{{Number: 1}, {Number: 2}}
	d := NewDiff(&fakeClient{prs: prs})
	got, err := d.Run(context.Background(), 1, "r", "a", "b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestDiff_PropagatesClientError(t *testing.T) {
	d := NewDiff(&fakeClient{err: errors.New("boom")})
	_, err := d.Run(context.Background(), 1, "r", "a", "b", nil)
	if err == nil {
		t.Fatal("want error")
	}
}

func TestDiff_CaseInsensitive(t *testing.T) {
	prs := []gh.MergedPR{
		{Number: 1, Title: "IFRAME handler tweak"},
	}
	d := NewDiff(&fakeClient{prs: prs})
	got, err := d.Run(context.Background(), 1, "r", "a", "b", []string{"iframe"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %d, want 1 (case-insensitive match)", len(got))
	}
}
