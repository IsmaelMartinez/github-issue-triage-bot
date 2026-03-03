package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

type ghIssue struct {
	Number      int              `json:"number"`
	Title       string           `json:"title"`
	State       string           `json:"state"`
	Body        string           `json:"body"`
	Labels      []ghLabel        `json:"labels"`
	CreatedAt   string           `json:"created_at"`
	ClosedAt    *string          `json:"closed_at"`
	Milestone   *ghMilestone     `json:"milestone"`
	PullRequest *json.RawMessage `json:"pull_request"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghMilestone struct {
	Title string `json:"title"`
}

type outputIssue struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	Labels    []string `json:"labels"`
	Summary   string   `json:"summary"`
	CreatedAt string   `json:"created_at"`
	ClosedAt  *string  `json:"closed_at"`
	Milestone *string  `json:"milestone"`
}

var (
	codeFenceRe = regexp.MustCompile("(?s)```.*?```")
	htmlTagRe   = regexp.MustCompile("<[^>]*>")
)

func stripBody(body string) string {
	s := codeFenceRe.ReplaceAllString(body, "")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}

func main() {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		fmt.Fprintf(os.Stderr, "GITHUB_TOKEN environment variable is required\n")
		os.Exit(1)
	}

	repo := "IsmaelMartinez/teams-for-linux"
	if len(os.Args) > 1 {
		repo = os.Args[1]
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var results []outputIssue

	for page := 1; ; page++ {
		url := fmt.Sprintf("https://api.github.com/repos/%s/issues?state=all&per_page=100&page=%d&direction=asc", repo, page)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating request: %v\n", err)
			os.Exit(1)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error fetching page %d: %v\n", page, err)
			os.Exit(1)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			fmt.Fprintf(os.Stderr, "GitHub API returned %d: %s\n", resp.StatusCode, string(body))
			os.Exit(1)
		}

		var issues []ghIssue
		if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
			resp.Body.Close()
			fmt.Fprintf(os.Stderr, "error decoding page %d: %v\n", page, err)
			os.Exit(1)
		}
		resp.Body.Close()

		if len(issues) == 0 {
			break
		}

		for _, iss := range issues {
			if iss.PullRequest != nil {
				continue
			}

			labels := make([]string, len(iss.Labels))
			for j, l := range iss.Labels {
				labels[j] = l.Name
			}

			out := outputIssue{
				Number:    iss.Number,
				Title:     iss.Title,
				State:     iss.State,
				Labels:    labels,
				Summary:   stripBody(iss.Body),
				CreatedAt: iss.CreatedAt,
				ClosedAt:  iss.ClosedAt,
			}
			if iss.Milestone != nil {
				out.Milestone = &iss.Milestone.Title
			}

			results = append(results, out)
		}

		fmt.Fprintf(os.Stderr, "page %d: fetched %d items, %d issues total\n", page, len(issues), len(results))

		if len(issues) < 100 {
			break
		}

		time.Sleep(500 * time.Millisecond)
	}

	fmt.Fprintf(os.Stderr, "export complete: %d issues\n", len(results))

	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		fmt.Fprintf(os.Stderr, "error encoding output: %v\n", err)
		os.Exit(1)
	}
}
